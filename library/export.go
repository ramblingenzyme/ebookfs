package library

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
)

func newExporter(cfg config.ReaderConfig, lib *libraryImpl) (Exporter, error) {
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
	name := book.JoinAuthors(b.Authors(), " & ")
	if fat, err := naming.ForFAT(name); err == nil {
		name = fat
	}
	return name
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
	lib *libraryImpl
}

func (e epubExporter) Open(b *Book) (EpubReader, error) {
	return epub.OpenReader(e.lib.store.AbsPath(b.EpubPath()), b.CoverPath())
}

func (e epubExporter) Size(b *Book) (int64, bool) {
	return b.EpubSize(), b.EpubSize() > 0
}

func (e epubExporter) Warm(*Book)              {}
func (e epubExporter) Filename(b *Book) string { return b.Filename() }

func (l *libraryImpl) Exporter(cfg config.ReaderConfig) (Exporter, error) {
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
