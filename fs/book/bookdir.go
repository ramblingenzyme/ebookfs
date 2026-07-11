// Package book holds the per-book 9P directory (BookDir) and the concrete files
// it assembles from the vfile primitives (cover/opf/epub/field, and the exported
// ReaderFile used by the reader view). It decouples from the registry via an
// injected edit callback, so it never imports registry or views.
package book

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// BookDir is the stable directory identity for one book. The book's state is
// held as an atomically swapped snapshot: 9P handlers run on many goroutines
// with no shared lock against registry commits, so they read an immutable
// *model.Book via Book() rather than fields mutated in place. Snapshots must
// never be modified after they are stored — an edit produces a fresh Book
// (library.Edit already does) and the registry swaps the pointer via SetSnapshot.
type BookDir struct {
	fs.StaticDir
	book atomic.Pointer[model.Book]
}

// Book returns the current snapshot. Callers needing a consistent view across
// several fields should call it once and read from the returned value.
func (d *BookDir) Book() *model.Book {
	return d.book.Load()
}

// SetSnapshot atomically replaces the book snapshot. The registry calls this
// under its own lock while bracketing the swap with view remove/add; handler
// goroutines read the pointer with Book() and never observe a torn value.
func (d *BookDir) SetSnapshot(b *model.Book) {
	d.book.Store(b)
}

// Stat reports the book's title as the entry name, recomputed live so a title
// edit shows up wherever the bare BookDir is listed (all-books, by-author). The
// Qid stays fixed, so a client with the epub open keeps its handle across a rename.
func (d *BookDir) Stat() proto.Stat {
	s := d.StaticDir.Stat()
	s.Name = d.Book().Title
	return s
}

type field struct {
	get func(*model.Book) string
	// edits converts string input to typed Edits. Error return is for input
	// parsing failures (e.g. strconv.Atoi); validation against the book's current
	// state is centralized in model.Edits.Validate, so this needs no snapshot.
	edits func(string) (model.Edits, error)
}

var fields = map[string]field{
	"status": {
		get: func(b *model.Book) string { return b.Meta.Status },
		edits: func(s string) (model.Edits, error) {
			return model.Edits{Status: &s}, nil
		},
	},
	"rating": {
		get: func(b *model.Book) string { return strconv.FormatFloat(b.Meta.Rating, 'f', -1, 64) },
		edits: func(s string) (model.Edits, error) {
			n, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return model.Edits{}, fmt.Errorf("invalid rating %q", s)
			}
			return model.Edits{Rating: &n}, nil
		},
	},
	"tags": {
		get: func(b *model.Book) string { return strings.Join(b.Meta.Tags, "\n") },
		edits: func(s string) (model.Edits, error) {
			tags := strings.FieldsFunc(s, func(r rune) bool { return r == '\n' })
			return model.Edits{Tags: &tags}, nil
		},
	},
	"title": {
		get: func(b *model.Book) string { return b.Title },
		edits: func(s string) (model.Edits, error) {
			return model.Edits{Title: &s}, nil
		},
	},
	"language": {
		get: func(b *model.Book) string { return b.Language },
		edits: func(s string) (model.Edits, error) {
			return model.Edits{Language: &s}, nil
		},
	},
	"description": {
		get: func(b *model.Book) string { return b.Description },
		edits: func(s string) (model.Edits, error) {
			return model.Edits{Description: &s}, nil
		},
	},
	"authors": {
		get: func(b *model.Book) string {
			names := make([]string, len(b.Authors))
			for i, a := range b.Authors {
				names[i] = a.Name
			}
			return strings.Join(names, "\n")
		},
		edits: func(s string) (model.Edits, error) {
			var names []string
			for _, n := range strings.Split(s, "\n") {
				if n = strings.TrimSpace(n); n != "" {
					names = append(names, n)
				}
			}
			authors := make([]model.Author, len(names))
			for i, n := range names {
				authors[i] = model.Author{Name: n}
			}
			return model.Edits{Authors: &authors}, nil
		},
	},
	"series": {
		get: func(b *model.Book) string {
			if b.Series == nil {
				return ""
			}
			return b.Series.Name
		},
		edits: func(s string) (model.Edits, error) {
			return model.Edits{Series: &s}, nil
		},
	},
	"series_index": {
		get: func(b *model.Book) string {
			if b.Series == nil {
				return ""
			}
			return strconv.FormatFloat(b.Series.Index, 'f', 1, 64)
		},
		edits: func(s string) (model.Edits, error) {
			idx, err := strconv.ParseFloat(s, 64)
			if err != nil {
				return model.Edits{}, fmt.Errorf("invalid series index %q", s)
			}
			return model.Edits{SeriesIndex: &idx}, nil
		},
	},
}

// NewBookDir builds the directory for a book. It takes the fs, the library
// facade, and an edit callback (the registry passes its own edit method) rather
// than the registry itself, so this package stays a leaf below the registry.
func NewBookDir(f *fs.FS, lib library.Library, edit func(int64, model.Edits) error, book *model.Book) *BookDir {
	d := &BookDir{
		StaticDir: *fs.NewStaticDir(newStat(f, book.Title, 0755|proto.DMDIR)),
	}
	d.book.Store(book)

	d.StaticDir.AddChild(fs.NewStaticFile(
		newStat(f, "id", 0444),
		fmt.Appendf(nil, "%d\n", book.Meta.ID),
	))

	// Child files read through d.Book so they always see the current snapshot,
	// not the one captured at construction.
	d.StaticDir.AddChild(newEpubFile(
		newStat(f, book.EpubFilename, 0444),
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
			return edit(d.Book().Meta.ID, edits)
		}
		d.StaticDir.AddChild(newFieldFile(newStat(f, name, 0644), get, set))
	}

	// Read-only bib fields.
	d.StaticDir.AddChild(newFieldFile(newStat(f, "pubdate", 0444), func() string { return d.Book().Pubdate }, nil))

	// Cover image — only present when the epub declares one.
	if book.CoverPath != "" {
		d.StaticDir.AddChild(newCoverFile(
			newStat(f, "cover"+filepath.Ext(book.CoverPath), 0644),
			lib,
			edit,
			d.Book,
		))
	}

	return d
}
