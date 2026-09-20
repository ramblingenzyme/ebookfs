package store

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestWalk(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author, A/Book One (1)", "Book One - A.epub", "book1", &book.Meta{ID: 1})
	writeBook(t, root, "Author, A/Book Two (2)", "Book Two - A.epub", "book2", &book.Meta{ID: 2})
	writeBook(t, root, "Author, B/Book Three (3)", "Book Three - B.epub", "book3", &book.Meta{ID: 3})
	// meta.toml present but no epub: skipped gracefully.
	writeBook(t, root, "Author, C/Stale (4)", "", "", &book.Meta{ID: 4})

	results, err := s.Walk()
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Walk returned %d entries, want 3", len(results))
	}

	found := make(map[string]bool)
	for _, loc := range results {
		found[loc.Dir()] = true
	}
	for _, want := range []string{"Author, A/Book One (1)", "Author, A/Book Two (2)", "Author, B/Book Three (3)"} {
		if !found[want] {
			t.Errorf("Walk did not return %q", want)
		}
	}
}

func TestWalkEmptyLibrary(t *testing.T) {
	s, _ := newStore(t)

	results, err := s.Walk()
	if err != nil {
		t.Fatalf("Walk on empty library: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Walk returned %d entries, want 0", len(results))
	}
}
