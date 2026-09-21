// Package book holds the per-book 9P directory (BookDir) and the concrete files
// it assembles from the vfile primitives (cover/opf/epub/field, and the exported
// ReaderFile used by the reader view).
package book

import (
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type ContentReader interface {
	Content(id int64) (library.EpubReader, error)
}

var (
	newStat    = vfile.NewStat
	newDirStat = vfile.NewDirStat
)

// BookDir is the stable directory identity for one book. The book snapshot is
// stored as an atomic.Pointer since 9P handlers run on concurrent goroutines
// without a shared lock for registry commits. Snapshots must remain immutable.
// Edits cause the pointer to be swapped.
type BookDir struct {
	fs.StaticDir
	book atomic.Pointer[library.Book]
}

// Book returns the current snapshot. Callers needing a consistent view across
// several fields should call it once and read from the returned value.
func (d *BookDir) Book() *library.Book {
	return d.book.Load()
}

// SetSnapshot atomically replaces the book snapshot. The registry calls this
// under its own lock while bracketing the swap with view remove/add; handler
// goroutines read the pointer with Book() and never observe a torn value.
func (d *BookDir) SetSnapshot(b *library.Book) {
	d.book.Store(b)
}

// Stat reports the book's title as the entry name, recomputed live so a title
// edit shows up wherever the bare BookDir is listed (all-books, by-author). The
// Qid stays fixed, so a client with the epub open keeps its handle across a rename.
func (d *BookDir) Stat() proto.Stat {
	s := d.StaticDir.Stat()
	s.Name = naming.PathSafe(d.Book().Title())
	return s
}

func NewBookDir(f *fs.FS, lib ContentReader, edit func(int64, library.Edits) error, book *library.Book) *BookDir {
	d := &BookDir{
		StaticDir: *fs.NewStaticDir(newDirStat(f, naming.PathSafe(book.Title()))),
	}
	d.book.Store(book)

	d.StaticDir.AddChild(fs.NewStaticFile(
		newStat(f, "id", 0444),
		fmt.Appendf(nil, "%d\n", book.ID()),
	))

	// Child files read through d.Book so they always see the current snapshot,
	// not the one captured at construction.
	d.StaticDir.AddChild(newEpubFile(
		newStat(f, book.Filename(), 0444),
		lib,
		d.Book,
	))

	d.StaticDir.AddChild(newOPFFile(
		newStat(f, "opf", 0444),
		lib,
		d.Book,
	))

	// Editable fields route through the edit callback so the change is validated,
	// persisted, and bracketed by view remove/add (rehoming if the grouping or
	// name changed). get reads the live book; set constructs Edits for the field.
	for name, fld := range fields {
		get := func() string { return fld.get(d.Book()) }
		set := func(s string) error {
			edits, err := fld.edits(s)
			if err != nil {
				return err
			}
			return edit(d.Book().ID(), edits)
		}
		d.StaticDir.AddChild(newFieldFile(newStat(f, name, 0644), get, set))
	}

	d.StaticDir.AddChild(newFieldFile(newStat(f, "pubdate", 0444), func() string { return d.Book().Pubdate() }, nil))
	d.StaticDir.AddChild(newFieldFile(newStat(f, "identifiers", 0444), func() string {
		return formatIdentifiers(d.Book().Identifiers())
	}, nil))

	if book.CoverPath() != "" {
		d.StaticDir.AddChild(newCoverFile(
			newStat(f, "cover"+filepath.Ext(book.CoverPath()), 0644),
			lib,
			edit,
			d.Book,
		))
	}

	return d
}
