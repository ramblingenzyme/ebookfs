package library

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type KepubCache struct {
	c *kepub.Cache
}

func NewKepubCache(dir string, lib Library) *KepubCache {
	return &KepubCache{c: kepub.NewCache(dir, lib)}
}

func (k *KepubCache) Open(b *model.Book) (EpubReader, error) { return k.c.Open(b) }
func (k *KepubCache) Size(b *model.Book) (int64, bool)      { return k.c.Size(b) }
func (k *KepubCache) Ensure(b *model.Book) error             { return k.c.Ensure(b) }
func (k *KepubCache) Filename(b *model.Book) string          { return k.c.Filename(b) }
func (k *KepubCache) Dirname(b *model.Book) string           { return exportDirname(b) }

type epubExporter struct {
	lib Library
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

func (e epubExporter) Ensure(*model.Book) error { return nil }

func (e epubExporter) Filename(b *model.Book) string { return b.EpubFilename }
func (e epubExporter) Dirname(b *model.Book) string  { return exportDirname(b) }

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
