package views

import (
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
)

type byAuthorDir struct{ groupingDir }

func NewByAuthorDir(reg *registry.BookRegistry) *byAuthorDir {
	d := &byAuthorDir{newGroupingDir(reg.FS(), "by-author")}
	reg.AddView(d)
	return d
}

func (d *byAuthorDir) Add(dir *book.BookDir) {
	for _, a := range dir.Book().Authors {
		d.listerDir(a.Name).Add(dir)
	}
}

func (d *byAuthorDir) Remove(dir *book.BookDir) {
	for _, a := range dir.Book().Authors {
		d.removeLister(a.Name, dir)
	}
}
