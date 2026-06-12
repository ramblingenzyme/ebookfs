package fs

import (
	"fmt"
	"strconv"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// seriesEntryDir wraps a shared *bookDir from the registry but presents it
// under a series-index-prefixed name within a series directory.
type seriesEntryDir struct {
	*bookDir
	entryStat proto.Stat
}

func (s *seriesEntryDir) Stat() proto.Stat { return s.entryStat }

func seriesEntryName(book *model.Book) string {
	index := strconv.FormatFloat(book.Series.Index, 'f', -1, 64)
	return fmt.Sprintf("%s. %s (%d)", index, book.Title, book.Meta.ID)
}

type seriesBookListDir struct {
	fs.StaticDir
	reg *bookRegistry
}

func (s *seriesBookListDir) add(book *model.Book) {
	name := seriesEntryName(book)
	stat := s.reg.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)
	s.StaticDir.AddChild(&seriesEntryDir{bookDir: s.reg.getOrCreate(book), entryStat: *stat})
}

type bySeriesDir struct {
	fs.StaticDir
	f   *fs.FS
	reg *bookRegistry
}

func newBySeriesDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *bySeriesDir {
	d := &bySeriesDir{
		StaticDir: *fs.NewStaticDir(f.NewStat("by-series", "glenda", "glenda", 0555|proto.DMDIR)),
		f:         f,
		reg:       reg,
	}
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
