package library

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/kepub"
)

// validateReaderConfig enforces ReaderConfig's invariants here rather than at
// whatever boundary the caller read the values from, because the struct is
// public and a caller may build one in Go.
func (l *Library) validateReaderConfig(cfg ReaderConfig) error {
	if !cfg.Convert {
		return nil
	}
	if cfg.CacheDir == "" {
		return errors.New("reader config: cache dir is required when converting")
	}
	// A cache inside the library root is walked as if it held books, so the
	// store would try to index converted kepubs.
	root := filepath.Clean(l.store.Root())
	dir := filepath.Clean(cfg.CacheDir)
	if dir == root || strings.HasPrefix(dir, root+string(filepath.Separator)) {
		return fmt.Errorf("reader config: cache dir %q must be outside the library root %q", dir, root)
	}
	return nil
}

func newExporter(cfg ReaderConfig, lib *Library) (Exporter, error) {
	if cfg.Convert {
		if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("creating kepub cache dir: %w", err)
		}
		return &kepubCache{
			readerPolicy: readerPolicy{statuses: cfg.Statuses},
			Cache:        kepub.NewCache(cfg.CacheDir, lib),
		}, nil
	}
	return epubExporter{readerPolicy: readerPolicy{statuses: cfg.Statuses}, lib: lib}, nil
}

// readerPolicy is the half of Exporter that decides *what* the reader view
// shows and how it groups, independent of the rendition served. Both exporters
// embed it so the rule has one definition rather than a copy each.
type readerPolicy struct {
	statuses []string
}

func (p readerPolicy) Includes(b *Book) bool {
	return slices.Contains(p.statuses, b.Status())
}

func (p readerPolicy) Dirname(b *Book) string {
	return naming.ForFAT(book.JoinAuthors(b.Authors(), book.AuthorSep))
}

// kepubCache serves converted kepubs. Close/Open/Size/Warm/Filename are the
// embedded cache's own methods.
type kepubCache struct {
	readerPolicy
	*kepub.Cache
}

func (k *kepubCache) Open(b *Book) (EpubReader, error) {
	return k.Cache.Open(book.Unwrap(b))
}

func (k *kepubCache) Size(b *Book) (int64, bool) {
	return k.Cache.Size(book.Unwrap(b))
}

func (k *kepubCache) Warm(b *Book) {
	k.Cache.Warm(book.Unwrap(b))
}

func (k *kepubCache) Filename(b *Book) string {
	return k.Cache.Filename(book.Unwrap(b))
}

type epubExporter struct {
	readerPolicy
	lib *Library
}

func (e epubExporter) Open(b *Book) (EpubReader, error) {
	return epub.OpenReader(e.lib.store.AbsPath(b.EpubPath()), b.CoverPath())
}

func (e epubExporter) Size(b *Book) (int64, bool) {
	return b.EpubSize(), b.EpubSize() > 0
}

func (e epubExporter) Warm(*Book)              {}
func (e epubExporter) Filename(b *Book) string { return b.Filename() }

// Exporter creates an export rendition for a reader view. Resources it holds
// are released by Library.Close, so the caller has no teardown to perform.
func (l *Library) Exporter(cfg ReaderConfig) (Exporter, error) {
	if err := l.validateReaderConfig(cfg); err != nil {
		return nil, err
	}
	e, err := newExporter(cfg, l)
	if err != nil {
		return nil, err
	}
	if c, ok := e.(io.Closer); ok {
		l.closerMu.Lock()
		l.closers = append(l.closers, c)
		l.closerMu.Unlock()
	}
	kind := "epub"
	if cfg.Convert {
		kind = "kepub"
	}
	slog.Info("export configured", "kind", kind, "statuses", cfg.Statuses)
	return e, nil
}
