// Package kepub builds and caches Kobo-format (kepub) renditions of books,
// layered on top of the library's epub access. It is the only package that
// depends on kepubify; nothing kepub-shaped reaches the library, store, or epub
// APIs — those treat it as an ordinary consumer.
package kepub

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubSource provides read access to a book's source epub. library.Library
// satisfies it via OpenEpub, which is the cache's only inbound dependency.
type EpubSource interface {
	OpenEpub(*model.Book) (model.EpubReader, error)
}

// Cache builds kepub renditions on demand and stores them on disk, so repeat
// reads (and rsyncs) are cheap. The cache directory lives OUTSIDE the library
// root, so the authoritative store and its reindex walk never see kepubs.
type Cache struct {
	dir string
	src EpubSource

	mu        sync.Mutex
	closeOnce sync.Once
	locks     map[int64]*sync.Mutex // per-book conversion lock, lazily created
	warmer    *warmer
}

func NewCache(dir string, src EpubSource) *Cache {
	c := &Cache{dir: dir, src: src, locks: make(map[int64]*sync.Mutex)}
	c.warmer = newWarmer(c.Ensure)
	return c
}

// Close stops the warmer goroutines and blocks until they finish.
func (c *Cache) Close() error {
	c.closeOnce.Do(func() {
		close(c.warmer.ch)
		c.warmer.wg.Wait()
	})
	return nil
}

// Warm is a non-blocking hint that b's kepub should be pro-actively cached.
// It enqueues the book; a full queue drops the hint and the read path still
// converts on demand.
func (c *Cache) Warm(b *model.Book) { c.warmer.warm(b) }

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
// *os.File satisfies model.EpubReader. This is the read-path backstop when the
// proactive warmer hasn't run (or its conversion is still in flight).
func (c *Cache) Open(b *model.Book) (model.EpubReader, error) {
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

	sfi, err := b.Stat()
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
func (c *Cache) write(b *model.Book, src model.EpubReader, size int64) error {
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

// warmer converts kepubs off the read path. The exporter's Warm method enqueues
// books here so their caches are built before the next rsync. Enqueue is
// non-blocking; a full queue drops the warm and the read path converts on demand.
type warmer struct {
	ensure func(*model.Book) error
	ch     chan *model.Book
	wg     sync.WaitGroup
}

func newWarmer(ensure func(*model.Book) error) *warmer {
	w := &warmer{ensure: ensure, ch: make(chan *model.Book, 4096)}
	w.wg.Add(4)
	for i := 0; i < 4; i++ {
		go w.run()
	}
	return w
}

func (w *warmer) warm(b *model.Book) {
	select {
	case w.ch <- b:
	default:
	}
}

func (w *warmer) run() {
	defer w.wg.Done()
	for b := range w.ch {
		if err := w.ensure(b); err != nil {
			log.Printf("kepub: warm book %d: %v", b.Meta.ID, err)
		}
	}
}
