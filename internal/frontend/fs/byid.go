package fs

import (
	"fmt"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

type byIDDir struct{ groupingDir }

func newByIDDir(reg *bookRegistry) *byIDDir {
	d := &byIDDir{newGroupingDir(reg.f, "by-id")}
	reg.AddView(d)
	return d
}

func idEntryName(b *model.Book) string {
	return fmt.Sprintf("%d. %s", b.Meta.ID, b.Title)
}

func (d *byIDDir) add(dir *bookDir) {
	stat := d.f.NewStat(idEntryName(dir.Book), "glenda", "glenda", 0555|proto.DMDIR)
	d.StaticDir.AddChild(&namedBookDir{bookDir: dir, baseStat: *stat, name: idEntryName})
}

func (d *byIDDir) remove(dir *bookDir) {
	d.StaticDir.DeleteChild(idEntryName(dir.Book))
}
