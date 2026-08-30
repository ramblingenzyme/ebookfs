package views

import (
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
)

type byAuthorDir struct{ groupingDir }

func NewByAuthorDir(reg *registry.BookRegistry) *byAuthorDir {
	d := &byAuthorDir{newGroupingDir(reg.FS(), "by-author")}
	reg.AddView(d)
	return d
}

// authorEntryName maps an author to their by-author directory name. Add and
// Remove must mint the same name or removals miss, so the mapping lives in one
// place — the same rule tagEntryName follows.
func authorEntryName(name string) string { return naming.PathSafe(name) }

func (d *byAuthorDir) Add(dir *book.BookDir) {
	for _, a := range dir.Book().Authors() {
		d.listerDir(authorEntryName(a.Name)).Add(dir)
	}
}

func (d *byAuthorDir) Remove(dir *book.BookDir) {
	for _, a := range dir.Book().Authors() {
		d.removeLister(authorEntryName(a.Name), dir)
	}
}
