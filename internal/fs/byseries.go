package fs

import (
	"fmt"
	"strconv"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

type seriesBookListDir struct {
	fs.StaticDir
	reg *bookRegistry
}

func (s *seriesBookListDir) add(book *model.Book) {
	index := strconv.FormatFloat(book.Series.Index, 'f', -1, 64)
	name := fmt.Sprintf("%s. %s", index, book.Title)
	stat := s.reg.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)
	s.StaticDir.AddChild(&namedBookDir{bookDir: s.reg.getOrCreate(book), entryStat: *stat})
}

type bySeriesDir struct{ groupingDir }

func newBySeriesDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *bySeriesDir {
	d := &bySeriesDir{newGroupingDir(f, reg, "by-series")}
	for _, book := range books {
		d.add(book)
	}
	return d
}

func (d *bySeriesDir) add(book *model.Book) {
	if book.Series == nil {
		return
	}
	key := book.Series.Name
	var seriesDir *seriesBookListDir
	if child, ok := d.Children()[key]; ok {
		seriesDir = child.(*seriesBookListDir)
	} else {
		seriesDir = &seriesBookListDir{
			StaticDir: *fs.NewStaticDir(d.f.NewStat(key, "glenda", "glenda", 0555|proto.DMDIR)),
			reg:       d.reg,
		}
		d.StaticDir.AddChild(seriesDir)
	}
	seriesDir.add(book)
}
