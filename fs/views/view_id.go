package views

import (
	"fmt"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type byIDDir struct{ groupingDir }

func NewByIDDir(reg *registry.BookRegistry) *byIDDir {
	d := &byIDDir{newGroupingDir(reg.FS(), "by-id")}
	reg.AddView(d)
	return d
}

func idEntryName(b *model.Book) string {
	return fmt.Sprintf("%d. %s", b.Meta.ID, b.Title)
}

func (d *byIDDir) Add(dir *book.BookDir) {
	stat := newStat(d.f, idEntryName(dir.Book()), 0555|proto.DMDIR)
	d.StaticDir.AddChild(&namedBookDir{BookDir: dir, baseStat: *stat, name: idEntryName})
}

func (d *byIDDir) Remove(dir *book.BookDir) {
	d.StaticDir.DeleteChild(idEntryName(dir.Book()))
}
