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
// An unrecorded size is unknown, so 9P never advertises an empty file.
func TestEpubExporterSize(t *testing.T) {
	for _, tc := range []struct {
		name     string
		recorded int64
		want     int64
		wantOK   bool
	}{
		{"recorded", 4242, 4242, true},
		{"unrecorded", 0, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := makeBook(1, "Test", "Author")
			b.EpubPath = "/nonexistent/missing.epub"
			b.EpubSize = tc.recorded
			size, ok := epubExporter{}.Size(util.WrapBook(b))
			if size != tc.want || ok != tc.wantOK {
				t.Errorf("Size = (%d, %v), want (%d, %v)", size, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestEpubExporterDirname(t *testing.T) {
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
