package views

import (
	"fmt"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
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
	f        *fs.FS
	books    map[int64]*book.BookDir
	children map[int64]*namedBookDir
	pad      atomic.Int32
}

// newSeriesBookListDir takes a prepared stat, matching newBookListDir, so
// groupingDir.childDir can construct it.
func newSeriesBookListDir(stat *proto.Stat, f *fs.FS) *seriesBookListDir {
	return &seriesBookListDir{
		StaticDir: *fs.NewStaticDir(stat),
		f:         f,
		books:     make(map[int64]*book.BookDir),
		children:  make(map[int64]*namedBookDir),
	}
}

func (s *seriesBookListDir) Add(dir *book.BookDir) {
	s.books[dir.Book().Meta.ID] = dir
	s.rebuild()
}

func (s *seriesBookListDir) Remove(dir *book.BookDir) {
	id := dir.Book().Meta.ID
	delete(s.books, id)
	// Drop the cached wrapper too, so a book removed from this series doesn't
	// linger in the cache for the life of the dir.
	delete(s.children, id)
	s.rebuild()
}

func (s *seriesBookListDir) rebuild() {
	var maxIdx float64
	for _, d := range s.books {
		if b := d.Book(); b.Series != nil && b.Series.Index > maxIdx {
			maxIdx = b.Series.Index
		}
	}
	s.pad.Store(0)
	if maxIdx >= 10 {
		s.pad.Store(2)
	}

	for name := range s.Children() {
		s.DeleteChild(name)
	}

	for _, d := range s.books {
		id := d.Book().Meta.ID
		n, ok := s.children[id]
		if !ok {
			stat := newStat(s.f, "", 0555|proto.DMDIR)
			n = &namedBookDir{
				BookDir:  d,
				baseStat: *stat,
				name:     func(b *model.Book) string { return seriesEntryName(b, int(s.pad.Load())) },
			}
			s.children[id] = n
		}
		s.AddChild(n)
	}
}

type bySeriesDir struct{ groupingDir }

func NewBySeriesDir(reg *registry.BookRegistry) *bySeriesDir {
	d := &bySeriesDir{newGroupingDir(reg.FS(), "by-series")}
	reg.AddView(d)
	return d
}

// seriesDir returns the subdir for a series name, creating it on first use.
func (d *bySeriesDir) seriesDir(name string) registry.BookView {
	return d.childDir(name, func(s *proto.Stat) fs.FSNode {
		return newSeriesBookListDir(s, d.f)
	}).(registry.BookView)
}

func (d *bySeriesDir) Add(dir *book.BookDir) {
	b := dir.Book()
	if b.Series == nil {
		return
	}
	d.seriesDir(b.Series.Name).Add(dir)
}

func (d *bySeriesDir) Remove(dir *book.BookDir) {
	b := dir.Book()
	if b.Series == nil {
		return
	}
	d.removeLister(b.Series.Name, dir)
}
