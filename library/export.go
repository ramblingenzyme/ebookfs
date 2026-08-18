package library

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
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

func (p readerPolicy) Includes(b *model.Book) bool {
	return slices.Contains(p.statuses, b.Meta.Status)
}

func (p readerPolicy) Dirname(b *model.Book) string {
	name := model.JoinAuthors(b.Authors, " & ")
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

type epubExporter struct {
	readerPolicy
	lib *libraryImpl
}

func (e epubExporter) Open(b *model.Book) (model.EpubReader, error) {
	return epub.OpenReader(e.lib.store.AbsPath(b.EpubPath), b.CoverPath)
}

func (e epubExporter) Size(b *model.Book) (int64, bool) {
	return b.EpubSize, b.EpubSize > 0
}

func (e epubExporter) Warm(*model.Book)              {}
func (e epubExporter) Filename(b *model.Book) string { return b.Filename() }

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
