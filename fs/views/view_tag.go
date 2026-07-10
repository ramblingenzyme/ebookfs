package views

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
)

type byTagDir struct{ groupingDir }

func NewByTagDir(reg *registry.BookRegistry) *byTagDir {
	d := &byTagDir{newGroupingDir(reg.FS(), "by-tag")}
	reg.AddView(d)
	return d
}

func (d *byTagDir) Add(dir *book.BookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		d.listerDir(name).Add(dir)
	}
}

func (d *byTagDir) Remove(dir *book.BookDir) {
	for _, tag := range dir.Book().Meta.Tags {
		if tag == "" {
			continue
		}
		name := strings.ReplaceAll(tag, "/", "_")
		d.removeLister(name, dir)
	}
}
