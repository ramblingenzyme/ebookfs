package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	// A deleted book whose removal failed: listing it would reindex it back.
	writeBook(t, root, trashDirName+"/x/Deleted (5)", "Deleted - D.epub", "book5", &book.Meta{ID: 5})

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

// A book directory a reader can list but not search fails meta.toml's stat
// with EACCES. Taken as "not a book", it dropped the book on reindex.
func TestWalkFailsOnUnsearchableBookDir(t *testing.T) {
	s, root := newStore(t)
	writeBook(t, root, "Author, A/Book One (1)", "Book One - A.epub", "book1", &book.Meta{ID: 1})
	dir := filepath.Join(root, "Author, A/Book One (1)")
	if err := os.Chmod(dir, 0644); err != nil {
		t.Skip("cannot chmod book dir:", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })
	if _, err := os.Stat(filepath.Join(dir, metaFilename)); err == nil {
		t.Skip("permissions not enforced, as for root")
	}

	if _, err := s.Walk(); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Walk: err = %v, want a permission error", err)
	}
}
