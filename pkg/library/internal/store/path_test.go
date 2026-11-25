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

	for _, tc := range []struct {
		name    string
		authors []book.Author
		title   string
		want    string
	}{
		{
			"single author",
			[]book.Author{{Name: "Alice"}},
			"My Title",
			"Alice/My Title (1)/My Title - Alice.epub",
		},
		{
			"two authors",
			[]book.Author{{Name: "Alice"}, {Name: "Bob"}},
			"The Title",
			"Alice & Bob/The Title (1)/The Title - Alice & Bob.epub",
		},
		{"nil authors", nil, "No Author", "Unknown/No Author (1)/No Author.epub"},
		{"empty authors", []book.Author{}, "No Author", "Unknown/No Author (1)/No Author.epub"},
		{
			"colon in title",
			[]book.Author{{Name: "Alice"}},
			"Title: Sub",
			"Alice/Title: Sub (1)/Title- Sub - Alice.epub",
		},
		{
			"slash in author",
			[]book.Author{{Name: "Alice/Author"}},
			"Title",
			"Alice-Author/Title (1)/Title - Alice-Author.epub",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.Layout(tc.authors, tc.title, 1).EpubPath; got != tc.want {
				t.Errorf("EpubPath = %q, want %q", got, tc.want)
			}
		})
	}
}

// A '/' in a title or an author name cannot split a library directory in two.
// An author name is read verbatim from the epub, so ".." is a value a file
// can carry, and filepath.Join would walk it out of the library root. "."
// collapses into the root instead. PathSafe prevents both.
func TestLayoutKeepsEachComponentInOneDirectory(t *testing.T) {
	s := layoutStore()

	for _, tc := range []struct {
		name    string
		authors []book.Author
		title   string
		id      int64
		wantDir string // empty means check escape properties instead
	}{
		{
			"slash",
			[]book.Author{{Name: "AC/DC"}},
			"Either/Or", 7,
			filepath.Join("AC-DC", "Either-Or (7)"),
		},
		{"dotdot", []book.Author{{Name: ".."}}, "Title", 5, ""},
		{"dot", []book.Author{{Name: "."}}, "Title", 5, ""},
		{"ellipsis", []book.Author{{Name: "..."}}, "Title", 5, ""},
		{"space", []book.Author{{Name: " "}}, "Title", 5, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			loc := s.Layout(tc.authors, tc.title, tc.id)
			if tc.wantDir != "" {
				if got := filepath.Dir(loc.EpubPath); got != tc.wantDir {
					t.Errorf("directory = %q, want %q", got, tc.wantDir)
				}

				return
			}

			if strings.HasPrefix(loc.EpubPath, ".") || strings.Contains(loc.EpubPath, ".."+string(filepath.Separator)) {
				t.Errorf("EpubPath %q leaves the library root", loc.EpubPath)
			}

			if got := len(strings.Split(filepath.Dir(loc.EpubPath), string(filepath.Separator))); got != 2 {
				t.Errorf("directory %q, want exactly two components", filepath.Dir(loc.EpubPath))
			}
		})
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
