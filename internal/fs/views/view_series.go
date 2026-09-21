package views

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// seriesEntryName builds a book's entry name within its series. The index is
// the string the epub carries, so it is used as written and only the first
// level is zero-padded. That is the level entries sort on, and the padding
// keeps "9" ahead of "10" in a plain lexical listing.
func seriesEntryName(b *library.Book, pad *padWidth) string {
	s := b.SeriesIndex()

	if pad.padded() {
		head, rest, _ := strings.Cut(s, ".")
		s = pad.format(int64(seriesLevel(head)))
		if rest != "" {
			s += "." + rest
		}
	}
	return fmt.Sprintf("%s - %s", s, naming.PathSafe(b.Title()))
}

// seriesLevel reads the first level of a series position, which is the only
// part the padding and the pad-width decision look at. A position that is not a
// number at all sorts as 0; edits.Validate rejects those on the way in,
// so this only covers what an epub already contained.
func seriesLevel(s string) int {
	head, _, _ := strings.Cut(s, ".")
	n, _ := strconv.Atoi(head)
	return n
}

// seriesBookListDir lists one series' books as namedBookDir entries. Entry
// names are computed live from the book snapshot and the current pad width
// (StaticDir keys children by Stat().Name dynamically), so Add and Remove only
// maintain membership and recompute the pad. A pad flip renames every entry
// in place without rebuilding children, keeping Qids and open fids stable.
type seriesBookListDir struct {
	fs.StaticDir
	f        *fs.FS
	children map[int64]*namedBookDir
	pad      padWidth
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
	n := newNamedBookDir(s.f, dir, func(b *library.Book) string {
		return seriesEntryName(b, &s.pad)
	})
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

// repad recomputes the width from the members still present, so a series that
// loses its highest-numbered volume narrows again.
func (s *seriesBookListDir) repad() {
	var maxIdx int
	for _, n := range s.children {
		b := n.Book()
		if !b.HasSeries() {
			continue
		}
		if level := seriesLevel(b.SeriesIndex()); level > maxIdx {
			maxIdx = level
		}
	}
	s.pad.set(int64(maxIdx))
}

func NewBySeriesDir(reg *registry.BookRegistry) *keyedDir {
	f := reg.FS()
	// The child carries its own listing rather than the shared one, to zero-pad
	// entries by series position.
	return newKeyedDir(reg, "by-series", seriesKeys, func(s *proto.Stat) fs.FSNode {
		return newSeriesBookListDir(s, f)
	})
}

// seriesKeys is the by-series entry b belongs under, or none when the book is
// in no series.
func seriesKeys(b *library.Book) []string {
	if !b.HasSeries() {
		return nil
	}
	return []string{b.SeriesName()}
}
