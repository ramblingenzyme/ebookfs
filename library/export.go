package library

import (
	"log"
	"os"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func NewExporter(cfg config.ReaderConfig, lib Library) Exporter {
	if cfg.Convert {
		if err := os.MkdirAll(cfg.CacheDir, 0755); err != nil {
			log.Fatalf("creating kepub cache dir: %v", err)
		}
		return &kepubCache{statuses: cfg.Statuses, c: kepub.NewCache(cfg.CacheDir, lib)}
	}
	return epubExporter{statuses: cfg.Statuses, lib: lib}
}

type kepubCache struct {
	statuses []string
	c        *kepub.Cache
}

func (k *kepubCache) Statuses() []string                     { return k.statuses }
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
	return e.lib.OpenEpub(b)
}

func (e epubExporter) Size(b *model.Book) (int64, bool) {
	fi, err := b.Stat()
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

func (e epubExporter) Warm(*model.Book)                 {}
func (e epubExporter) Statuses() []string               { return e.statuses }
func (e epubExporter) Filename(b *model.Book) string    { return b.EpubFilename }
func (e epubExporter) Dirname(b *model.Book) string     { return exportDirname(b) }

func exportDirname(b *model.Book) string {
	var names []string
	for _, a := range b.Authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	name := "Unknown"
	if len(names) > 0 {
		name = strings.Join(names, " & ")
	}
	if fat, err := naming.ForFAT(name); err == nil {
		name = fat
	}
	return name
}
