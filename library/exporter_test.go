package library

import (
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library/internal/kepub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

var makeBook = testutil.MakeBook

// TestEpubExporter_Size_ReportsRecordedSize pins that Size answers from the size
// recorded at index time rather than the filesystem — the book's path points at
// nothing, and the call must still succeed without touching disk. Every indexed
// book was stat'd on the way in, so a missing file surfaces at Open rather than
// as a length the exporter has to guess at.
func TestEpubExporter_Size_ReportsRecordedSize(t *testing.T) {
	book := makeBook(1, "Test", "Author")
	book.EpubPath = "/nonexistent/missing.epub"
	book.EpubSize = 4242

	exp := epubExporter{}
	size, ok := exp.Size(book)
	if !ok {
		t.Error("Size should be known for any indexed book")
	}
	if size != 4242 {
		t.Errorf("Size = %d, want 4242", size)
	}
}

// TestEpubExporter_Size_Unrecorded covers the other side of that guard. A book
// carrying no recorded size was never observed, and reporting 0 as authoritative
// would have 9P advertise a zero-length file and export sizing believe it — so
// the size reads as unknown and the caller falls back rather than trusting it.
func TestEpubExporter_Size_Unrecorded(t *testing.T) {
	book := makeBook(1, "Test", "Author") // EpubSize left at its zero value

	exp := epubExporter{}
	size, ok := exp.Size(book)
	if ok {
		t.Errorf("Size = (%d, true) for a book with no recorded size, want it reported as unknown", size)
	}
}

func TestEpubExporter_Filename(t *testing.T) {
	exp := epubExporter{}
	book := makeBook(1, "Test", "Author")
	book.EpubPath = "mybook.epub"

	name := exp.Filename(book)
	if name != "mybook.epub" {
		t.Errorf("Filename = %q, want %q", name, "mybook.epub")
	}
}

func TestEpubExporter_Warm(t *testing.T) {
	exp := epubExporter{}
	exp.Warm(nil)
}

// TestExporterIncludes runs the status filter over both exporters. They carry
// separate copies of the same one-line rule, and it decides what a reader mount
// can see, so a divergence between them is a mount quietly serving the wrong
// set of books. The kepub cache is built without its converter: Includes reads
// only the configured statuses.
func TestExporterIncludes(t *testing.T) {
	exporters := map[string]func(statuses []string) Exporter{
		"epub":  func(s []string) Exporter { return epubExporter{statuses: s} },
		"kepub": func(s []string) Exporter { return &kepubCache{statuses: s} },
	}

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
	for kind, newExp := range exporters {
		t.Run(kind, func(t *testing.T) {
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					book := makeBook(1, "Test", "Author")
					book.Meta.Status = tt.status
					if got := newExp(tt.statuses).Includes(book); got != tt.want {
						t.Errorf("Includes = %v, want %v", got, tt.want)
					}
				})
			}
		})
	}
}

type dummyKepubSource struct{}

func (dummyKepubSource) Content(int64) (model.EpubReader, error) {
	return nil, errors.New("dummy source: no epub")
}

func TestKepubCacheDelegates(t *testing.T) {
	dir := t.TempDir()
	kc := &kepubCache{
		statuses: []string{"reading"},
		c:        kepub.NewCache(dir, dummyKepubSource{}),
	}

	b := makeBook(1, "Test", "Alice")
	b.EpubPath = "mybook.epub"

	if s := kc.statuses; len(s) != 1 || s[0] != "reading" {
		t.Errorf("Statuses = %v, want [reading]", s)
	}

	if fn := kc.Filename(b); fn != "mybook.kepub.epub" {
		t.Errorf("Filename = %q, want %q", fn, "mybook.kepub.epub")
	}

	if dn := kc.Dirname(b); dn != "Alice" {
		t.Errorf("Dirname = %q, want %q", dn, "Alice")
	}

	_, ok := kc.Size(b)
	if ok {
		t.Error("Size should report cold for missing cache file")
	}

	// Warm must not panic.
	kc.Warm(b)

	// Open returns an error because the dummy source returns nil.
	_, err := kc.Open(b)
	if err == nil {
		t.Error("expected error from Open with dummy source")
	}

	// Close stops the warmer without error.
	if err := kc.Close(); err != nil {
		t.Errorf("close: %v", err)
	}
}

func TestEpubExporter_Dirname(t *testing.T) {
	tests := []struct {
		name     string
		authorFn func() []model.Author
		want     string
	}{
		{"single author", func() []model.Author { return []model.Author{{Name: "Alice"}} }, "Alice"},
		{"two authors", func() []model.Author { return []model.Author{{Name: "Alice"}, {Name: "Bob"}} }, "Alice & Bob"},
		{"multiple authors", func() []model.Author { return []model.Author{{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}} }, "Alice & Bob & Carol"},
		{"empty author name", func() []model.Author { return []model.Author{{Name: ""}} }, model.UnknownAuthor},
		{"mixed empty and valid", func() []model.Author { return []model.Author{{Name: ""}, {Name: "Alice"}} }, "Alice"},
		{"no authors", func() []model.Author { return nil }, model.UnknownAuthor},
		{"colon in name", func() []model.Author { return []model.Author{{Name: "Title: Sub"}} }, "Title- Sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp := epubExporter{}
			authors := tt.authorFn()
			names := make([]string, len(authors))
			for i, a := range authors {
				names[i] = a.Name
			}
			b := makeBook(1, "Test", names...)
			b.Authors = authors
			got := exp.Dirname(b)
			if got != tt.want {
				t.Errorf("Dirname = %q, want %q", got, tt.want)
			}
		})
	}
}
