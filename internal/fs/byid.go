package fs

import (
	"fmt"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

type byIDDir struct{ groupingDir }

func newByIDDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *byIDDir {
	d := &byIDDir{newGroupingDir(f, reg, "by-id")}
	for _, book := range books {
		d.add(book)
	}
	return d
}

func (d *byIDDir) add(book *model.Book) {
	name := fmt.Sprintf("%s. %s", book.Meta.ID, book.Title)
	stat := d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)
	d.StaticDir.AddChild(&namedBookDir{bookDir: d.reg.getOrCreate(book), entryStat: *stat})
}
