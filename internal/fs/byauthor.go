package fs

import (
	"github.com/knusbaum/go9p/proto"
)

type byAuthorDir struct{ groupingDir }

func newByAuthorDir(reg *bookRegistry) *byAuthorDir {
	d := &byAuthorDir{newGroupingDir(reg.f, "by-author")}
	reg.AddView(d)
	return d
}

// authorDir returns the subdir for an author name, creating it on first use.
// TODO: prune a subdir once its last book leaves (e.g. after an author rename).
func (d *byAuthorDir) authorDir(name string) *booksDir {
	if child, ok := d.Children()[name]; ok {
		return child.(*booksDir)
	}
	ad := newBooksDir(d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR))
	d.StaticDir.AddChild(ad)
	return ad
}

func (d *byAuthorDir) add(dir *bookDir) {
	for _, a := range dir.Book.Authors {
		d.authorDir(a.Name).add(dir)
	}
}

func (d *byAuthorDir) remove(dir *bookDir) {
	for _, a := range dir.Book.Authors {
		if child, ok := d.Children()[a.Name]; ok {
			child.(*booksDir).remove(dir)
		}
	}
}
