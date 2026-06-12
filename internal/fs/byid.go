package fs

import (
	"strconv"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

type idEntryDir struct {
	*bookDir
	entryStat proto.Stat
}

func (e *idEntryDir) Stat() proto.Stat { return e.entryStat }

type byIDDir struct {
	fs.StaticDir
	f   *fs.FS
	reg *bookRegistry
}

func newByIDDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *byIDDir {
	d := &byIDDir{
		StaticDir: *fs.NewStaticDir(f.NewStat("by-id", "glenda", "glenda", 0555|proto.DMDIR)),
		f:         f,
		reg:       reg,
	}
	for _, book := range books {
		d.add(book)
	}
	return d
}

func (d *byIDDir) add(book *model.Book) {
	name := strconv.FormatInt(book.Meta.ID, 10)
	stat := d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)
	d.StaticDir.AddChild(&idEntryDir{bookDir: d.reg.getOrCreate(book), entryStat: *stat})
}
