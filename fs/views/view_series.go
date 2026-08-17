package views

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func seriesEntryName(b *model.Book, pad int32) string {
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

// seriesBookListDir lists one series' books as namedBookDir entries. Entry
// names are computed live from the book snapshot and the current pad width
// (StaticDir keys children by Stat().Name dynamically), so Add and Remove only
// maintain membership and recompute the pad — a pad flip renames every entry
// in place without rebuilding children, keeping Qids and open fids stable.
type seriesBookListDir struct {
	fs.StaticDir
	f        *fs.FS
	children map[int64]*namedBookDir
	pad      atomic.Int32
}

// newSeriesBookListDir takes a prepared stat, matching newBookListDir, so
// groupingDir.childDir can construct it.
func newSeriesBookListDir(stat *proto.Stat, f *fs.FS) *seriesBookListDir {
	return &seriesBookListDir{
		StaticDir: *fs.NewStaticDir(stat),
		f:         f,
		children:  make(map[int64]*namedBookDir),
	}
}

func (s *seriesBookListDir) Add(dir *book.BookDir) {
	n := &namedBookDir{
		BookDir:  dir,
		baseStat: *newStat(s.f, "", 0555|proto.DMDIR),
		name:     func(b *model.Book) string { return seriesEntryName(b, s.pad.Load()) },
	}
	s.children[dir.Book().Meta.ID] = n
	s.StaticDir.AddChild(n)
	s.repad()
}

func (s *seriesBookListDir) Remove(dir *book.BookDir) {
	id := dir.Book().Meta.ID
	n, ok := s.children[id]
	if !ok {
		return
	}
	// Delete under the entry's current live name, before repad can change it.
	s.StaticDir.DeleteChild(n.Stat().Name)
	delete(s.children, id)
	s.repad()
}

// repad recomputes the zero-pad width from the current members: two digits
// once any series index reaches 10.
func (s *seriesBookListDir) repad() {
	var maxIdx float64
	for _, n := range s.children {
		if b := n.Book(); b.HasSeries() && b.Series.Index > maxIdx {
			maxIdx = b.Series.Index
		}
	}
	var pad int32
	if maxIdx >= 10 {
		pad = 2
	}
	s.pad.Store(pad)
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
	if !b.HasSeries() {
		return
	}
	d.seriesDir(b.SeriesName()).Add(dir)
}

func (d *bySeriesDir) Remove(dir *book.BookDir) {
	b := dir.Book()
	if !b.HasSeries() {
		return
	}
	d.removeLister(b.SeriesName(), dir)
}
