// Package kepub builds and caches Kobo-format (kepub) renditions of books,
// layered on top of the library's epub access. It is the only package that
// depends on kepubify. Nothing kepub-shaped reaches the library or epub APIs,
// which treat it as an ordinary consumer.
package kepub

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/util/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

type EpubSource interface {
	Content(int64) (epub.EpubReader, error)
}

// Cache writes kepub sidecar files into each book's .sidecar/ directory.
type Cache struct {
	pathFn func(id int64, name string) (string, error)
	src    EpubSource

	locks  syncutil.KeyedMutex // per-book conversion lock
	warmer *warmer

	// ctx is cancelled by Close. kepubify honours it, so an in-flight
	// conversion aborts instead of holding shutdown open for as long as one
	// book takes to convert. Close is called after the 9P server is already
	// down, outside main's shutdown deadline.
	ctx    context.Context
	cancel context.CancelFunc

	convertFn func(context.Context, io.Writer, io.ReaderAt, int64) error
}

func NewCache(pathFn func(id int64, name string) (string, error), src EpubSource) *Cache {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{
		pathFn:    pathFn,
		src:       src,
		convertFn: convert,
		ctx:       ctx,
		cancel:    cancel,
	}
	c.warmer = newWarmer(ctx, c.Ensure)
	return c
}

// Close drops the queued warms rather than draining them, and blocks until the
// warmer goroutines finish. Repeat calls are safe.
func (c *Cache) Close() error {
	c.cancel()
	c.warmer.wait()
	return nil
}

// Warm is a non-blocking hint.
func (c *Cache) Warm(b *book.ImmutableBook) { c.warmer.warm(b) }

// Filename is FAT-safe because the store already sanitized the epub filename
// it is built from.
func (c *Cache) Filename(b *book.ImmutableBook) string {
	return strings.TrimSuffix(b.Filename(), ".epub") + ".kepub.epub"
}

// Size answers the 9P stat length, so it must not convert.
func (c *Cache) Size(b *book.ImmutableBook) (int64, bool) {
	path, err := c.path(b)
	if err != nil {
		return 0, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// Open is the read-path backstop for when the warmer has not run, or its
// conversion is still in flight.
func (c *Cache) Open(b *book.ImmutableBook) (epub.EpubReader, error) {
	if err := c.Ensure(b); err != nil {
		return nil, err
	}
	path, err := c.path(b)
	if err != nil {
		return nil, err
	}
	return epub.OpenReader(path, b.CoverPath())
}

// Ensure is serialized per book, so concurrent warms and reads coalesce into a
// single conversion.
func (c *Cache) Ensure(b *book.ImmutableBook) error {
	l := c.locks.For(b.ID())
	l.Lock()
	defer l.Unlock()

	path, err := c.path(b)
	if err != nil {
		return err
	}

	// An in-place epub rewrite updates DateModified, which is what makes a
	// cache older than it stale.
	if cfi, err := os.Stat(path); err == nil && !cfi.ModTime().Before(b.DateModified()) {
		return nil
	}

	content, err := c.src.Content(b.ID())
	if err != nil {
		return err
	}
	defer content.Close()

	return c.write(b, path, content)
}

func (c *Cache) path(b *book.ImmutableBook) (string, error) {
	return c.pathFn(b.ID(), "kepub.epub")
}

// write renames into place, so a reader never observes a partial kepub.
func (c *Cache) write(b *book.ImmutableBook, path string, src epub.EpubReader) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, fmt.Sprintf(".%d-*.tmp", b.ID()))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed; cleans up on any error path

	if err := c.convertFn(c.ctx, tmp, src, b.EpubSize()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
