package library

import (
	"fmt"
	"os"
	"slices"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func newExporter(cfg config.ReaderConfig, lib Library) (Exporter, error) {
	if cfg.Convert {
		if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
			return nil, fmt.Errorf("creating kepub cache dir: %w", err)
		}
		return &kepubCache{statuses: cfg.Statuses, c: kepub.NewCache(cfg.CacheDir, lib)}, nil
	}
	return epubExporter{statuses: cfg.Statuses, lib: lib}, nil
}

type kepubCache struct {
	statuses []string
	c        *kepub.Cache
}

func (k *kepubCache) close() error                           { return k.c.Close() }
func (k *kepubCache) Includes(b *model.Book) bool            { return slices.Contains(k.statuses, b.Meta.Status) }
func (k *kepubCache) Open(b *model.Book) (EpubReader, error) { return k.c.Open(b) }
func (k *kepubCache) Size(b *model.Book) (int64, bool)       { return k.c.Size(b) }
func (k *kepubCache) Warm(b *model.Book)                     { k.c.Warm(b) }
func (k *kepubCache) Filename(b *model.Book) string          { return k.c.Filename(b) }
func (k *kepubCache) Dirname(b *model.Book) string           { return exportDirname(b) }

type epubExporter struct {
	statuses []string
	lib      Library
}

func (e epubExporter) Open(b *model.Book) (EpubReader, error) {
	return e.lib.OpenEpub(b.Meta.ID)
}

func (e epubExporter) Size(b *model.Book) (int64, bool) {
	return b.EpubSize, b.EpubSize > 0
}

func (e epubExporter) Warm(*model.Book)              {}
func (e epubExporter) Includes(b *model.Book) bool   { return slices.Contains(e.statuses, b.Meta.Status) }
func (e epubExporter) Filename(b *model.Book) string { return b.EpubFilename }
func (e epubExporter) Dirname(b *model.Book) string  { return exportDirname(b) }

func exportDirname(b *model.Book) string {
	name := model.JoinAuthors(b.Authors, " & ")
	if fat, err := naming.ForFAT(name); err == nil {
		name = fat
	}
	return name
}
