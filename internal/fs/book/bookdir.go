// Package book holds the per-book 9P directory (BookDir) and the concrete files
// it assembles from the vfile primitives (cover/opf/epub/field, and the exported
// ReaderFile used by the reader view). It decouples from the registry via an
// injected edit callback, so it never imports registry or views.
package book

import (
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library"
)

// ContentReader is the half of the library this package uses: the book files
// are rendered from the epub the library opens by id.
type ContentReader interface {
	Content(id int64) (library.EpubReader, error)
}

// newStat is the package-local shorthand for vfile.NewStat, the single
// definition of the glenda/glenda owner convention every node uses.
var newStat = vfile.NewStat

// BookDir is the stable directory identity for one book. The book's state is
// held as an atomically swapped snapshot: 9P handlers run on many goroutines
// with no shared lock against registry commits, so they read an immutable
// *library.Book via Book() rather than fields mutated in place. Snapshots must
// never be modified after they are stored — an edit produces a fresh Book
// (library.Edit already does) and the registry swaps the pointer via SetSnapshot.
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
	// PathSafe because a title is stored as the epub wrote it and a 9P entry
	// name is a single component.
	s.Name = naming.PathSafe(d.Book().Title())
	return s
}

// NewBookDir builds the directory for a book. It takes the fs, the library
// facade, and an edit callback (the registry passes its own edit method) rather
// than the registry itself, so this package stays a leaf below the registry.
func NewBookDir(f *fs.FS, lib ContentReader, edit func(int64, library.Edits) error, book *library.Book) *BookDir {
	d := &BookDir{
		StaticDir: *fs.NewStaticDir(newStat(f, naming.PathSafe(book.Title()), 0755|proto.DMDIR)),
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

	// Read-only bib fields.
	d.StaticDir.AddChild(newFieldFile(newStat(f, "pubdate", 0444), func() string { return d.Book().Pubdate() }, nil))
	d.StaticDir.AddChild(newFieldFile(newStat(f, "identifiers", 0444), func() string {
		return formatIdentifiers(d.Book().Identifiers())
	}, nil))

	// Cover image — only present when the epub declares one.
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
