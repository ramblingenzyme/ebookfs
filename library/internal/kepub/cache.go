// Package kepub builds and caches Kobo-format (kepub) renditions of books,
// layered on top of the library's epub access. It is the only package that
// depends on kepubify; nothing kepub-shaped reaches the library, store, or epub
// APIs — those treat it as an ordinary consumer.
package kepub

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// EpubSource provides read access to a book's source epub content.
// library.Library satisfies it via Content, which is the cache's only
// inbound dependency.
type EpubSource interface {
	Content(int64) (model.EpubReader, error)
}

// Cache builds kepub renditions on demand and stores them on disk, so repeat
// reads (and rsyncs) are cheap. The cache directory lives OUTSIDE the library
// root, so the authoritative store and its reindex walk never see kepubs.
type Cache struct {
	dir string
	src EpubSource

	closeOnce sync.Once
	locks     syncutil.KeyedMutex // per-book conversion lock
	warmer    *warmer

	// convertFn is the epub-to-kepub converter. Defaults to convert;
	// overridable in tests to avoid the kepubify dependency.
	convertFn func(context.Context, io.Writer, io.ReaderAt, int64) error
}

func NewCache(dir string, src EpubSource) *Cache {
	c := &Cache{
		dir:       dir,
		src:       src,
		convertFn: convert,
	}
	c.warmer = newWarmer(c.Ensure)
	return c
}

// Close stops the warmer goroutines and blocks until they finish.
func (c *Cache) Close() error {
	c.closeOnce.Do(c.warmer.stop)
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
	return strings.TrimSuffix(b.Filename(), ".epub") + ".kepub.epub"
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
	l := c.locks.For(b.Meta.ID)
	l.Lock()
	defer l.Unlock()
	return c.ensureLocked(b)
}

// Open ensures b's kepub is fresh, then opens it for reading. The returned
// *epub.Reader satisfies model.EpubReader. This is the read-path backstop when
// the proactive warmer hasn't run (or its conversion is still in flight).
func (c *Cache) Open(b *model.Book) (model.EpubReader, error) {
	if err := c.Ensure(b); err != nil {
		return nil, err
	}
	return epub.OpenReader(c.path(b), b.CoverPath)
}

func (c *Cache) ensureLocked(b *model.Book) error {
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

// write converts src into a temp file in the cache dir, then atomically renames
// it into place so a reader never observes a partial kepub.
func (c *Cache) write(b *model.Book, src model.EpubReader) error {
	tmp, err := os.CreateTemp(c.dir, fmt.Sprintf(".%d-*.tmp", b.Meta.ID))
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed; cleans up on any error path

	if err := c.convertFn(context.Background(), tmp, src, b.EpubSize); err != nil {
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

const (
	warmerGoroutines = 4    // concurrent conversions
	warmerQueueSize  = 4096 // max backlog before drops
)

// warmer converts kepubs off the read path. The exporter's Warm method enqueues
// books here so their caches are built before the next rsync. Enqueue is
// non-blocking; a full queue drops the warm and the read path converts on demand.
type warmer struct {
	ensure func(*model.Book) error
	ch     chan *model.Book
	wg     sync.WaitGroup

	// mu guards closed and, crucially, brackets the channel send in warm so it
	// can never race stop's close(ch) — a send on a closed channel panics even
	// inside a select. An atomic flag would not suffice: the check and the send
	// must be one critical section against the close.
	mu     sync.Mutex
	closed bool
}

func newWarmer(ensure func(*model.Book) error) *warmer {
	w := &warmer{ensure: ensure, ch: make(chan *model.Book, warmerQueueSize)}
	w.wg.Add(warmerGoroutines)
	for i := 0; i < warmerGoroutines; i++ {
		go w.run()
	}
	return w
}

func (w *warmer) warm(b *model.Book) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed {
		return // stopped; drop the hint
	}
	select {
	case w.ch <- b:
	default: // full; drop the hint
	}
}

// stop closes the queue and blocks until the warmer goroutines drain it and
// exit. After stop returns (or once closed is set), warm drops hints instead of
// sending, so it never touches the closed channel.
func (w *warmer) stop() {
	w.mu.Lock()
	w.closed = true
	close(w.ch)
	w.mu.Unlock()
	w.wg.Wait()
}

func (w *warmer) run() {
	defer w.wg.Done()
	for b := range w.ch {
		if err := w.ensure(b); err != nil {
			slog.Warn("kepub: warm book failed", "book_id", b.Meta.ID, "error", err)
		}
	}
}
