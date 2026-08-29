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
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// seriesEntryName builds a book's entry name within its series. The index is
// the string the epub carries, so it is used as written and only the first
// level is zero-padded — that is the level entries sort on, and padding it is
// what keeps "9" ahead of "10" in a plain lexical listing.
func seriesEntryName(b *library.Book, pad int32) string {
	s := b.SeriesIndex()

	if pad > 0 {
		head, rest, _ := strings.Cut(s, ".")
		s = fmt.Sprintf("%02d", seriesLevel(head))
		if rest != "" {
			s += "." + rest
		}
	}
	return fmt.Sprintf("%s - %s", s, model.PathSafe(b.Title()))
}

// seriesLevel reads the first level of a series position, which is the only
// part the padding and the pad-width decision look at. A position that is not a
// number at all sorts as 0; model.Edits.Validate rejects those on the way in,
// so this only covers what an epub already contained.
func seriesLevel(s string) int {
	head, _, _ := strings.Cut(s, ".")
	n, _ := strconv.Atoi(head)
	return n
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
		name:     func(b *library.Book) string { return seriesEntryName(b, s.pad.Load()) },
	}
	s.children[dir.Book().ID()] = n
	s.StaticDir.AddChild(n)
	s.repad()
}

func (s *seriesBookListDir) Remove(dir *book.BookDir) {
	id := dir.Book().ID()
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
	var maxIdx int
	for _, n := range s.children {
		if b := n.Book(); b.HasSeries() && seriesLevel(b.SeriesIndex()) > maxIdx {
			maxIdx = seriesLevel(b.SeriesIndex())
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
// PathSafe for the same reason as by-author and by-tag: a series name is
// metadata read verbatim from the epub, and a '/' in one would make an entry a
// 9P client cannot walk to. Remove must mint the same name or removals miss.
func (d *bySeriesDir) seriesDir(name string) registry.BookView {
	return d.childDir(model.PathSafe(name), func(s *proto.Stat) fs.FSNode {
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
	d.removeLister(model.PathSafe(b.SeriesName()), dir)
}
