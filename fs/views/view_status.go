package views

import (
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
)

type byStatusDir struct{ groupingDir }

func NewByStatusDir(reg *registry.BookRegistry) *byStatusDir {
	d := &byStatusDir{newGroupingDir(reg.FS(), "by-status")}
	reg.AddView(d)
	return d
}

func (d *byStatusDir) Add(dir *book.BookDir) {
	d.listerDir(dir.Book().Status()).Add(dir)
}

func (d *byStatusDir) Remove(dir *book.BookDir) {
	d.removeLister(dir.Book().Status(), dir)
}
