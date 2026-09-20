// Package kepub builds and caches Kobo-format (kepub) renditions of books,
// layered on top of the library's epub access. It is the only package that
// depends on kepubify; nothing kepub-shaped reaches the library, store, or epub
// APIs — those treat it as an ordinary consumer.
//
// Nothing here calls itself: the consumer is the kepubCache wrapper in
// library/export.go, which adapts these methods to the library.Exporter the 9P
// reader/ view is built on. Two of its calls produce a rendition, and both
// funnel through Ensure, which holds the freshness rule:
//
//	Open  → Ensure → write → kepubify   (a 9P read, converting on demand)
//	Warm  → warmer → Ensure → …         (off the read path, see warmer.go)
//
// The rest — Size, Filename — answer from the cache directory without
// converting, because 9P stats every file it lists.
package kepub

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// EpubSource provides read access to a book's source epub content.
// library.Library satisfies it via Content, which is the cache's only
// inbound dependency.
type EpubSource interface {
	Content(int64) (epub.EpubReader, error)
}

// Cache builds kepub renditions on demand and stores them on disk, so repeat
// reads (and rsyncs) are cheap. The cache directory lives OUTSIDE the library
// root, so the authoritative store and its reindex walk never see kepubs.
type Cache struct {
	dir string
	src EpubSource

	locks  syncutil.KeyedMutex // per-book conversion lock
	warmer *warmer

	// ctx is cancelled by Close. kepubify honours it, so an in-flight
	// conversion aborts instead of holding shutdown open for as long as one
	// book takes to convert — Close is called after the 9P server is already
	// down, outside main's shutdown deadline.
	ctx    context.Context
	cancel context.CancelFunc

	// convertFn is the epub-to-kepub converter. Defaults to convert;
	// overridable in tests to avoid the kepubify dependency.
	convertFn func(context.Context, io.Writer, io.ReaderAt, int64) error
}

func NewCache(dir string, src EpubSource) *Cache {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{
		dir:       dir,
		src:       src,
		convertFn: convert,
		ctx:       ctx,
		cancel:    cancel,
	}
	c.warmer = newWarmer(ctx, c.Ensure)
	return c
}

// Close cancels conversions and blocks until the warmer goroutines finish.
// Queued warms are dropped rather than drained: a warm is a hint, and the read
// path converts on demand. Repeat calls are safe, and Warm after one is a
// no-op that reaches no converter.
func (c *Cache) Close() error {
	c.cancel()
	c.warmer.wait()
	return nil
}

// Warm is a non-blocking hint that b's kepub should be pro-actively cached.
// It enqueues the book; a full queue drops the hint and the read path still
// converts on demand.
func (c *Cache) Warm(b *book.Book) { c.warmer.warm(b) }

// Filename is the FAT-safe export name for b's kepub: the epub filename (already
// sanitized by the store) with its .epub suffix replaced by .kepub.epub.
func (c *Cache) Filename(b *book.Book) string {
	return strings.TrimSuffix(b.Filename(), ".epub") + ".kepub.epub"
}

// Size reports the cached kepub's size without converting; ok is false when the
// cache is cold. Used for the 9P stat length, so it must stay cheap.
func (c *Cache) Size(b *book.Book) (int64, bool) {
	fi, err := os.Stat(c.path(b))
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// Open ensures b's kepub is fresh, then opens it for reading. This is the
// read-path backstop when the proactive warmer hasn't run (or its conversion is
// still in flight).
func (c *Cache) Open(b *book.Book) (epub.EpubReader, error) {
	if err := c.Ensure(b); err != nil {
		return nil, err
	}
	return epub.OpenReader(c.path(b), b.CoverPath)
}

// Ensure builds b's kepub if the cache is missing or stale, leaving a fresh
// rendition on disk. It is idempotent (a fresh cache is a no-op) and serialized
// per book, so concurrent warms and reads coalesce into a single conversion.
func (c *Cache) Ensure(b *book.Book) error {
	l := c.locks.For(b.Meta.ID)
	l.Lock()
	defer l.Unlock()

	// Fresh iff the cache exists and is no older than the book's last
	// modification time. An in-place epub rewrite updates DateModified,
	// which invalidates the cached rendition.
	if cfi, err := os.Stat(c.path(b)); err == nil && !cfi.ModTime().Before(b.Meta.DateModified) {
		return nil
	}

	content, err := c.src.Content(b.Meta.ID)
	if err != nil {
		return err
	}
	defer content.Close()

	return c.write(b, content)
}

func (c *Cache) path(b *book.Book) string {
	return filepath.Join(c.dir, fmt.Sprintf("%d.kepub.epub", b.Meta.ID))
}

// write converts src into a temp file in the cache dir, then atomically renames
// it into place so a reader never observes a partial kepub.
func (c *Cache) write(b *book.Book, src epub.EpubReader) error {
	tmp, err := os.CreateTemp(c.dir, fmt.Sprintf(".%d-*.tmp", b.Meta.ID))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed; cleans up on any error path

	if err := c.convertFn(c.ctx, tmp, src, b.EpubSize); err != nil {
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
	return os.Rename(tmpName, c.path(b))
}
