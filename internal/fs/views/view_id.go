package views

import (
	"fmt"
	"sync/atomic"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/util/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func idEntryName(b *library.Book, pad *padWidth) string {
	return fmt.Sprintf("%s. %s", pad.format(b.ID()), naming.PathSafe(b.Title()))
}

type byIDDir struct {
	groupingDir
	maxID atomic.Int64
	pad   padWidth
}

func NewByIDDir(reg *registry.BookRegistry) *byIDDir {
	d := &byIDDir{
		groupingDir: newGroupingDir(reg.FS(), "by-id"),
	}
	reg.AddView(d)
	return d
}

func (d *byIDDir) Add(dir *book.BookDir) {
	id := dir.Book().ID()
	if id > d.maxID.Load() {
		d.maxID.Store(id)
		d.pad.set(id)
	}
	d.StaticDir.AddChild(newNamedBookDir(d.f, dir, func(b *library.Book) string {
		return idEntryName(b, &d.pad)
	}))
}

func (d *byIDDir) Remove(dir *book.BookDir) {
	d.StaticDir.DeleteChild(idEntryName(dir.Book(), &d.pad))
}
