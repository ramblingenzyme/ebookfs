package store

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Layout and the names under it read no Store field, so these need no root on
// disk. newStore would create a temp directory none of them touch.
func layoutStore() *Store { return New("") }

func TestLayout(t *testing.T) {
	s := layoutStore()

	loc := s.Layout([]book.Author{{Name: "Alice"}}, "My Title", 1)
	wantRel := filepath.Join("Alice/My Title (1)", "My Title - Alice.epub")
	if loc.EpubPath != wantRel {
		t.Errorf("EpubPath = %q, want %q", loc.EpubPath, wantRel)
	}
}

// A '/' in a title or an author name cannot split a library directory in two.
// The epub package reports metadata as the file wrote it (EPUB 3.3 §5.5.2), so a
// title like "Either/Or" reaches Layout intact and this is the only thing
// standing between it and a stray nested directory.
func TestLayoutSlashInTitleStaysOneDirectory(t *testing.T) {
	s := layoutStore()

	loc := s.Layout([]book.Author{{Name: "AC/DC"}}, "Either/Or", 7)
	dir := filepath.Dir(loc.EpubPath)
	if got := strings.Count(dir, string(filepath.Separator)); got != 1 {
		t.Errorf("directory = %q, want exactly two components, got %d separators", dir, got)
	}
	if want := filepath.Join("AC-DC", "Either-Or (7)"); dir != want {
		t.Errorf("directory = %q, want %q", dir, want)
	}
}

// PathSafe's other guarantee. An author name is read verbatim from the epub, so
// ".." is a value a file can carry, and filepath.Join would walk it out of the
// library root: the book written outside the library entirely, with ingest, move
// and delete all then operating on the escaped path. "." collapses into the root
// instead.
func TestLayoutCannotEscapeTheLibraryRoot(t *testing.T) {
	s := layoutStore()

	for _, name := range []string{"..", ".", "...", " "} {
		loc := s.Layout([]book.Author{{Name: name}}, "Title", 5)
		if strings.HasPrefix(loc.EpubPath, ".") || strings.Contains(loc.EpubPath, ".."+string(filepath.Separator)) {
			t.Errorf("author %q gave EpubPath %q, which leaves the library root", name, loc.EpubPath)
		}
		if got := len(strings.Split(filepath.Dir(loc.EpubPath), string(filepath.Separator))); got != 2 {
			t.Errorf("author %q gave directory %q, want exactly two components", name, filepath.Dir(loc.EpubPath))
		}
	}
}

func TestLayoutUnknownAuthor(t *testing.T) {
	s := layoutStore()

	loc := s.Layout(nil, "Untitled", 99)
	if loc.EpubPath != filepath.Join("Unknown/Untitled (99)", "Untitled.epub") {
		t.Errorf("EpubPath = %q, want %q", loc.EpubPath, filepath.Join("Unknown/Untitled (99)", "Untitled.epub"))
	}
}

func TestCanonicalDir(t *testing.T) {
	tests := []struct {
		name    string
		authors []book.Author
		title   string
		id      int64
		want    string
	}{
		{"basic", []book.Author{{Name: "Alice"}}, "The Title", 42, "Alice/The Title (42)"},
		{"two authors", []book.Author{{Name: "Alice"}, {Name: "Bob"}}, "The Title", 42, "Alice & Bob/The Title (42)"},
		{"unknown author", nil, "No Author", 1, "Unknown/No Author (1)"},
		{"title with id", []book.Author{{Name: "Bob"}}, "My Book", 7, "Bob/My Book (7)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := canonicalDir(tt.authors, tt.title, tt.id)
			if got != tt.want {
				t.Errorf("canonicalDir(%v, %q, %d) = %q, want %q", tt.authors, tt.title, tt.id, got, tt.want)
			}
		})
	}
}

func TestEpubFilename(t *testing.T) {
	tests := []struct {
		name    string
		authors []book.Author
		title   string
		want    string
	}{
		{"single author", []book.Author{{Name: "Alice"}}, "Wonderful Title", "Wonderful Title - Alice.epub"},
		{"two authors", []book.Author{{Name: "Alice"}, {Name: "Bob"}}, "Title", "Title - Alice & Bob.epub"},
		{"no authors", nil, "No Author Book", "No Author Book.epub"},
		{"empty authors", []book.Author{}, "Empty Authors", "Empty Authors.epub"},
		{"colon in title", []book.Author{{Name: "Alice"}}, "Title: Sub", "Title- Sub - Alice.epub"},
		{"slash in author", []book.Author{{Name: "Alice/Author"}}, "Title", "Title - Alice-Author.epub"},
		{"leading dot trimmed", []book.Author{{Name: "Alice"}}, ".hidden", "hidden - Alice.epub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := epubFilename(tt.authors, tt.title)
			if got != tt.want {
				t.Errorf("epubFilename(%v, %q) = %q, want %q", tt.authors, tt.title, got, tt.want)
			}
		})
	}
}

func TestEpubFilenameForFFallback(t *testing.T) {
	// A title that is only dots triggers ForFAT to return an error (trimmed to
	// empty), exercising the fallback to the raw title.
	got := epubFilename([]book.Author{{Name: "Alice"}}, ".")
	if got != ". - Alice.epub" {
		t.Errorf("epubFilename = %q, want %q", got, ". - Alice.epub")
	}
}

// The inverse of canonicalDir's " (id)" suffix. It is the only way to recover a
// book's id when meta.toml cannot be parsed, so it has to reject anything it is
// not certain about rather than guess.
func TestIDFromPath(t *testing.T) {
	tests := []struct {
		path string
		want int64
		ok   bool
	}{
		// The three layouts this project has used.
		{"Alice/Test Title (1)", 1, true},
		{"Alice & Bob/Test Title (42)", 42, true},
		{"Smith, Alice/Test Title (7)", 7, true},
		// A title that itself ends in parentheses: the last group wins.
		{"Alice/Test Title (Annotated) (9)", 9, true},
		// Nothing to read.
		{"Alice/Test Title", 0, false},
		{"Alice", 0, false},
		{"", 0, false},
		// Present but not a usable id.
		{"Alice/Test Title ()", 0, false},
		{"Alice/Test Title (abc)", 0, false},
		{"Alice/Test Title (0)", 0, false},
		{"Alice/Test Title (-3)", 0, false},
		// Unclosed or malformed.
		{"Alice/Test Title (12", 0, false},
		{"Alice/(5)", 0, false},
	}
	for _, tc := range tests {
		got, ok := IDFromPath(tc.path)
		if got != tc.want || ok != tc.ok {
			t.Errorf("IDFromPath(%q) = (%d, %v), want (%d, %v)", tc.path, got, ok, tc.want, tc.ok)
		}
	}
}
