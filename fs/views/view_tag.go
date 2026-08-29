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

// tagEntryName maps a tag to its by-tag directory name. Add and Remove must
// mint the same name or removals miss, so the mapping lives in one place.
func tagEntryName(tag string) string {
	return strings.ReplaceAll(tag, "/", "_")
}

func (d *byTagDir) Add(dir *book.BookDir) {
	for _, tag := range dir.Book().Tags() {
		if tag == "" {
			continue
		}
		d.listerDir(tagEntryName(tag)).Add(dir)
	}
}

func (d *byTagDir) Remove(dir *book.BookDir) {
	for _, tag := range dir.Book().Tags() {
		if tag == "" {
			continue
		}
		d.removeLister(tagEntryName(tag), dir)
	}
}
