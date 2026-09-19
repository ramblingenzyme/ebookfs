// The exporter predicates: what a reader mount shows, what it calls a book, and
// what size it reports. All three answer from the book record and the configured
// statuses, with no library and no disk, so they are tested against the concrete
// types directly. lib.Exporter's wiring is covered black-box in
// export_ext_test.go.

package library

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

var makeBook = testutil.MakeMutableBook

// The status filter runs over both renditions. They carry separate copies of the
// same one-line rule, and it decides what a reader mount can see, so a
// divergence between them is a mount quietly serving the wrong set of books.
func TestExporterIncludes(t *testing.T) {
	tests := []struct {
		name     string
		statuses []string
		status   string
		want     bool
	}{
		{"matching status", []string{"unread", "reading"}, "unread", true},
		{"non-matching status", []string{"unread", "reading"}, "archived", false},
		{"empty statuses", nil, "unread", false},
		{"empty book status", []string{"unread"}, "", false},
		{"empty book status with empty in statuses", []string{""}, "", true},
	}
	for kind, newExp := range map[string]func([]string) Exporter{
		"epub":  func(s []string) Exporter { return epubExporter{readerPolicy: readerPolicy{statuses: s}} },
		"kepub": func(s []string) Exporter { return &kepubCache{readerPolicy: readerPolicy{statuses: s}} },
	} {
		t.Run(kind, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					b := makeBook(1, "Test", "Author")
					b.Meta.Status = tt.status
					exp := newExp(tt.statuses)
					if got := exp.Includes(testutil.WrapBook(b)); got != tt.want {
						t.Errorf("Includes = %v, want %v", got, tt.want)
					}
				})
			}
		})
	}
}

// Size answers from the size recorded at index time rather than the filesystem:
// the book's path points at nothing, and the call must still succeed without
// touching disk. Every indexed book was stat'd on the way in, so a missing file
// surfaces at Open rather than as a length the exporter has to guess at.
func TestEpubExporter_Size_ReportsRecordedSize(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "/nonexistent/missing.epub"
	b.EpubSize = 4242

	size, ok := epubExporter{}.Size(testutil.WrapBook(b))
	if !ok {
		t.Error("Size should be known for any indexed book")
	}
	if size != 4242 {
		t.Errorf("Size = %d, want 4242", size)
	}
}

// A book carrying no recorded size was never observed, and reporting 0 as
// authoritative would have 9P advertise a zero-length file and export sizing
// believe it, so the size reads as unknown and the caller falls back rather
// than trusting it.
func TestEpubExporter_Size_Unrecorded(t *testing.T) {
	b := makeBook(1, "Test", "Author") // EpubSize left at its zero value

	size, ok := epubExporter{}.Size(testutil.WrapBook(b))
	if ok {
		t.Errorf("Size = (%d, true) for a book with no recorded size, want it reported as unknown", size)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "mybook.epub"

	name := epubExporter{}.Filename(testutil.WrapBook(b))
	if name != "mybook.epub" {
		t.Errorf("Filename = %q, want %q", name, "mybook.epub")
	}
}

func TestEpubExporter_Warm(t *testing.T) {
	epubExporter{}.Warm(nil)
}

func TestEpubExporter_Dirname(t *testing.T) {
	tests := []struct {
		name     string
		authorFn func() []Author
		want     string
	}{
		{"single author", func() []Author { return []Author{{Name: "Alice"}} }, "Alice"},
		{"two authors", func() []Author { return []Author{{Name: "Alice"}, {Name: "Bob"}} }, "Alice & Bob"},
		{"multiple authors", func() []Author {
			return []Author{{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}}
		}, "Alice & Bob & Carol"},
		{"empty author name", func() []Author { return []Author{{Name: ""}} }, book.UnknownAuthor},
		{"mixed empty and valid", func() []Author { return []Author{{Name: ""}, {Name: "Alice"}} }, "Alice"},
		{"no authors", func() []Author { return nil }, book.UnknownAuthor},
		{"colon in name", func() []Author { return []Author{{Name: "Title: Sub"}} }, "Title- Sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authors := tt.authorFn()
			names := make([]string, len(authors))
			for i, a := range authors {
				names[i] = a.Name
			}
			b := makeBook(1, "Test", names...)
			b.Authors = authors
			got := epubExporter{}.Dirname(testutil.WrapBook(b))
			if got != tt.want {
				t.Errorf("Dirname = %q, want %q", got, tt.want)
			}
		})
	}
}
