package fs

import (
	"fmt"
	"strconv"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func seriesEntryName(b *model.Book) string {
	index := strconv.FormatFloat(b.Series.Index, 'f', -1, 64)
	return fmt.Sprintf("%s. %s", index, b.Title)
}

type seriesBookListDir struct {
	fs.StaticDir
	f *fs.FS
}

func (s *seriesBookListDir) add(dir *bookDir) {
	stat := s.f.NewStat(seriesEntryName(dir.Book), "glenda", "glenda", 0555|proto.DMDIR)
	s.StaticDir.AddChild(&namedBookDir{bookDir: dir, baseStat: *stat, name: seriesEntryName})
}

func (s *seriesBookListDir) remove(dir *bookDir) {
	s.StaticDir.DeleteChild(seriesEntryName(dir.Book))
}

type bySeriesDir struct{ groupingDir }

func newBySeriesDir(reg *bookRegistry) *bySeriesDir {
	d := &bySeriesDir{newGroupingDir(reg.f, "by-series")}
	reg.AddView(d)
	return d
}

// seriesDir returns the subdir for a series name, creating it on first use.
// TODO: prune a subdir once its last book leaves (e.g. after a series change).
func (d *bySeriesDir) seriesDir(name string) *seriesBookListDir {
	if child, ok := d.Children()[name]; ok {
		return child.(*seriesBookListDir)
	}
	sd := &seriesBookListDir{
		StaticDir: *fs.NewStaticDir(d.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)),
		f:         d.f,
	}
	d.StaticDir.AddChild(sd)
	return sd
}

func (d *bySeriesDir) add(dir *bookDir) {
	if dir.Book.Series == nil {
		return
	}
	d.seriesDir(dir.Book.Series.Name).add(dir)
}

func (d *bySeriesDir) remove(dir *bookDir) {
	if dir.Book.Series == nil {
		return
	}
	if child, ok := d.Children()[dir.Book.Series.Name]; ok {
		child.(*seriesBookListDir).remove(dir)
	}
}
