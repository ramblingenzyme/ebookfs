// Package kepub builds and caches Kobo-format (kepub) renditions of books,
// layered on top of the library's epub access. It is the only package that
// depends on kepubify; nothing kepub-shaped reaches the library, store, or epub
// APIs — those treat it as an ordinary consumer.
package kepub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// EpubSource provides read access to a book's source epub. *library.Library
// satisfies it via OpenEpub, which is the cache's only inbound dependency.
type EpubSource interface {
	OpenEpub(*model.Book) (library.EpubReader, error)
}

// Cache builds kepub renditions on demand and stores them on disk, so repeat
// reads (and rsyncs) are cheap. The cache directory lives OUTSIDE the library
// root, so the authoritative store and its reindex walk never see kepubs.
type Cache struct {
	dir string
	src EpubSource

	mu    sync.Mutex
	locks map[int64]*sync.Mutex // per-book conversion lock, lazily created
}

func NewCache(dir string, src EpubSource) *Cache {
	return &Cache{dir: dir, src: src, locks: make(map[int64]*sync.Mutex)}
}

func (c *Cache) path(b *model.Book) string {
	return filepath.Join(c.dir, fmt.Sprintf("%d.kepub.epub", b.Meta.ID))
}

// Filename is the FAT-safe export name for b's kepub: the epub filename (already
// sanitized by the store) with its .epub suffix replaced by .kepub.epub.
func (c *Cache) Filename(b *model.Book) string {
	return strings.TrimSuffix(b.EpubFilename, ".epub") + ".kepub.epub"
}

// Size reports the cached kepub's size without converting; ok is false when the
// cache is cold. Used for the 9P stat length, so it must stay cheap.
func (c *Cache) Size(b *model.Book) (int64, bool) {
	fi, err := os.Stat(c.path(b))
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// Ensure builds b's kepub if the cache is missing or stale, leaving a fresh
// rendition on disk. It is idempotent (a fresh cache is a no-op) and serialized
// per book, so concurrent warms and reads coalesce into a single conversion.
func (c *Cache) Ensure(b *model.Book) error {
	l := c.lockFor(b.Meta.ID)
	l.Lock()
	defer l.Unlock()
	return c.ensureLocked(b)
}

// Open ensures b's kepub is fresh, then opens it for reading. The returned
// *os.File satisfies library.EpubReader. This is the read-path backstop when the
// proactive warmer hasn't run (or its conversion is still in flight).
func (c *Cache) Open(b *model.Book) (library.EpubReader, error) {
	if err := c.Ensure(b); err != nil {
		return nil, err
	}
	return os.Open(c.path(b))
}

func (c *Cache) ensureLocked(b *model.Book) error {
	src, err := c.src.OpenEpub(b)
	if err != nil {
		return err
	}
	defer src.Close()

	sfi, err := src.Stat()
	if err != nil {
		return err
	}
	// Fresh iff the cache exists and is no older than the source epub. Keying on
	// the source mtime means an in-place epub rewrite invalidates the kepub.
	if cfi, err := os.Stat(c.path(b)); err == nil && !cfi.ModTime().Before(sfi.ModTime()) {
		return nil
	}

	return c.write(b, src, sfi.Size())
}

// write converts src into a temp file in the cache dir, then atomically renames
// it into place so a reader never observes a partial kepub.
func (c *Cache) write(b *model.Book, src library.EpubReader, size int64) error {
	tmp, err := os.CreateTemp(c.dir, fmt.Sprintf(".%d-*.tmp", b.Meta.ID))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed; cleans up on any error path

	if err := convert(context.Background(), tmp, src, size); err != nil {
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

func (c *Cache) lockFor(id int64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[id]
	if !ok {
		l = &sync.Mutex{}
		c.locks[id] = l
	}
	return l
}
