package fs

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func seriesEntryName(b *model.Book, pad int) string {
	s := strconv.FormatFloat(b.Series.Index, 'f', 1, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")

	if pad > 0 {
		parts := strings.SplitN(s, ".", 2)
		intPart, _ := strconv.Atoi(parts[0])
		s = fmt.Sprintf("%02d", intPart)
		if len(parts) > 1 {
			s += "." + parts[1]
		}
	}
	return fmt.Sprintf("%s - %s", s, b.Title)
}

func seriesEntryNameFunc(pad int) func(*model.Book) string {
	return func(b *model.Book) string { return seriesEntryName(b, pad) }
}

type seriesBookListDir struct {
	fs.StaticDir
	f     *fs.FS
	books map[int64]*bookDir
}

func newSeriesBookListDir(f *fs.FS, name string) *seriesBookListDir {
	return &seriesBookListDir{
		StaticDir: *fs.NewStaticDir(f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)),
		f:         f,
		books:     make(map[int64]*bookDir),
	}
}

func (s *seriesBookListDir) add(dir *bookDir) {
	s.books[dir.Book.Meta.ID] = dir
	s.rebuild()
}

func (s *seriesBookListDir) remove(dir *bookDir) {
	delete(s.books, dir.Book.Meta.ID)
	s.rebuild()
}

func (s *seriesBookListDir) rebuild() {
	var maxIdx float64
	for _, d := range s.books {
		if d.Book.Series != nil && d.Book.Series.Index > maxIdx {
			maxIdx = d.Book.Series.Index
		}
	}
	pad := 0
	if maxIdx >= 10 {
		pad = 2
	}

	for name := range s.Children() {
		s.DeleteChild(name)
	}

	nameFn := seriesEntryNameFunc(pad)
	for _, d := range s.books {
		name := nameFn(d.Book)
		stat := s.f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)
		s.AddChild(&namedBookDir{bookDir: d, baseStat: *stat, name: nameFn})
	}
}

type bySeriesDir struct{ groupingDir }

func newBySeriesDir(reg *bookRegistry) *bySeriesDir {
	d := &bySeriesDir{newGroupingDir(reg.f, "by-series")}
	reg.AddView(d)
	return d
}

// seriesDir returns the subdir for a series name, creating it on first use.
func (d *bySeriesDir) seriesDir(name string) bookLister {
	if child, ok := d.Children()[name]; ok {
		return child.(bookLister)
	}
	sd := newSeriesBookListDir(d.f, name)
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
		child.(bookLister).remove(dir)
		d.pruneEmpty(dir.Book.Series.Name)
	}
}
