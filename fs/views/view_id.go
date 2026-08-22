package views

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func idEntryName(b *model.Book, pad int) string {
	if pad > 0 {
		return fmt.Sprintf("%0*d. %s", pad, b.Meta.ID, model.PathSafe(b.Title))
	}
	return fmt.Sprintf("%d. %s", b.Meta.ID, model.PathSafe(b.Title))
}

type byIDDir struct {
	groupingDir
	maxID atomic.Int64
	pad   atomic.Int32
}

func NewByIDDir(reg *registry.BookRegistry) *byIDDir {
	d := &byIDDir{
		groupingDir: newGroupingDir(reg.FS(), "by-id"),
	}
	reg.AddView(d)
	return d
}

func (d *byIDDir) Add(dir *book.BookDir) {
	id := dir.Book().Meta.ID
	if id > d.maxID.Load() {
		d.maxID.Store(id)
		d.updatePad(id)
	}
	n := &namedBookDir{
		BookDir:  dir,
		baseStat: *newStat(d.f, "", 0555|proto.DMDIR),
		name:     func(b *model.Book) string { return idEntryName(b, int(d.pad.Load())) },
	}
	d.StaticDir.AddChild(n)
}

func (d *byIDDir) Remove(dir *book.BookDir) {
	d.StaticDir.DeleteChild(idEntryName(dir.Book(), int(d.pad.Load())))
}

// updatePad widens the zero-padding to the digit count of maxID, so by-id
// entries sort lexically. A single-digit library needs no padding at all.
func (d *byIDDir) updatePad(maxID int64) {
	var pad int32
	if maxID >= 10 {
		pad = int32(len(strconv.FormatInt(maxID, 10)))
	}
	d.pad.Store(pad)
}
