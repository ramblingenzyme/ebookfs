package library

import (
	"io"
	"log/slog"
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/util/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/kepub"
)

func newExporter(cfg ReaderConfig, lib *Library) (Exporter, error) {
	if cfg.Convert {
		pathFn := func(id int64, name string) (string, error) {
			b, err := lib.get(id)
			if err != nil {
				return "", err
			}
			return lib.store.SidecarPath(b.Location, name)
		}
		return &kepubCache{
			readerPolicy: readerPolicy{statuses: cfg.Statuses},
			Cache:        kepub.NewCache(pathFn, lib),
		}, nil
	}
	return epubExporter{readerPolicy: readerPolicy{statuses: cfg.Statuses}, lib: lib}, nil
}

// readerPolicy is the half of Exporter that decides which books the reader
// view shows and how it groups them, whatever the rendition.
type readerPolicy struct {
	statuses []string
}

func (p readerPolicy) Includes(b *Book) bool {
	return slices.Contains(p.statuses, b.Status())
}

func (p readerPolicy) Dirname(b *Book) string {
	return naming.ForFAT(book.JoinAuthors(b.Authors(), book.AuthorSep))
}

// kepubCache satisfies Exporter by embedding alone: kepub.Cache has the
// rendition methods and Close, readerPolicy the rest.
type kepubCache struct {
	readerPolicy
	*kepub.Cache
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

// Exporter creates the rendition for a reader view. Library.Close releases it,
// so the caller has nothing to close.
func (l *Library) Exporter(cfg ReaderConfig) (Exporter, error) {
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
