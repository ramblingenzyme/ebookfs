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
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/util/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

type EpubSource interface {
	Content(int64) (epub.EpubReader, error)
}

// SidecarHost provides access to a book's sidecar directory.
type SidecarHost interface {
	WithSidecars(id int64, fn func(*os.Root) error) error
}

// CacheHost combines SidecarHost and EpubSource. A Library satisfies both.
type CacheHost interface {
	SidecarHost
	EpubSource
}

// Cache writes kepub sidecar files into each book's .sidecar/ directory.
type Cache struct {
	host CacheHost

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

func NewCache(host CacheHost) *Cache {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Cache{
		host:      host,
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
	var size int64
	err := c.host.WithSidecars(b.ID(), func(root *os.Root) error {
		fi, err := root.Stat("kepub.epub")
		if err != nil {
			return err
		}
		size = fi.Size()
		return nil
	})
	if err != nil {
		return 0, false
	}
	return size, true
}

// Open is the read-path backstop for when the warmer has not run, or its
// conversion is still in flight.
func (c *Cache) Open(b *book.ImmutableBook) (epub.EpubReader, error) {
	if err := c.Ensure(b); err != nil {
		return nil, err
	}
	var reader epub.EpubReader
	err := c.host.WithSidecars(b.ID(), func(root *os.Root) error {
		f, err := root.Open("kepub.epub")
		if err != nil {
			return err
		}
		r, err := epub.OpenReaderFromFile(f, "kepub.epub", b.CoverPath())
		if err != nil {
			f.Close()
			return err
		}
		reader = r
		return nil
	})
	return reader, err
}

// Ensure is serialized per book, so concurrent warms and reads coalesce into a
// single conversion.
func (c *Cache) Ensure(b *book.ImmutableBook) error {
	l := c.locks.For(b.ID())
	l.Lock()
	defer l.Unlock()

	return c.host.WithSidecars(b.ID(), func(root *os.Root) error {
		// An in-place epub rewrite updates DateModified, which is what makes a
		// cache older than it stale.
		if cfi, err := root.Stat("kepub.epub"); err == nil && !cfi.ModTime().Before(b.DateModified()) {
			return nil
		}

		content, err := c.host.Content(b.ID())
		if err != nil {
			return err
		}
		defer content.Close()

		return c.write(b, root, content)
	})
}

// write renames into place, so a reader never observes a partial kepub.
func (c *Cache) write(b *book.ImmutableBook, root *os.Root, src epub.EpubReader) error {
	// Deterministic name: the per-book lock in Ensure serializes writes, so
	// there is no contention on the temp name within a single book.
	tmpName := fmt.Sprintf(".kepub.%d.tmp", b.ID())
	tmp, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	defer root.Remove(tmpName) // no-op once renamed; cleans up on any error path

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
	return root.Rename(tmpName, "kepub.epub")
}
