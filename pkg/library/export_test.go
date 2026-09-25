package library

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

var makeBook = util.MakeMutableBook

// Both renditions reach Includes by embedding readerPolicy. One with its own
// Includes would show the wrong books.
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
					if got := exp.Includes(util.WrapBook(b)); got != tt.want {
						t.Errorf("Includes = %v, want %v", got, tt.want)
					}
				})
			}
		})
	}
}

// Size answers from the size recorded at index time, without touching disk.
func TestEpubExporter_Size_ReportsRecordedSize(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "/nonexistent/missing.epub"
	b.EpubSize = 4242

	size, ok := epubExporter{}.Size(util.WrapBook(b))
	if !ok {
		t.Error("Size should be known for any indexed book")
	}
	if size != 4242 {
		t.Errorf("Size = %d, want 4242", size)
	}
}

// An unrecorded size reads as unknown, so 9P does not advertise a zero-length
// file.
func TestEpubExporter_Size_Unrecorded(t *testing.T) {
	b := makeBook(1, "Test", "Author") // EpubSize left at its zero value

	size, ok := epubExporter{}.Size(util.WrapBook(b))
	if ok {
		t.Errorf("Size = (%d, true) for a book with no recorded size, want it reported as unknown", size)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	b := makeBook(1, "Test", "Author")
	b.EpubPath = "mybook.epub"

	name := epubExporter{}.Filename(util.WrapBook(b))
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
			got := epubExporter{}.Dirname(util.WrapBook(b))
			if got != tt.want {
				t.Errorf("Dirname = %q, want %q", got, tt.want)
			}
		})
	}
}
