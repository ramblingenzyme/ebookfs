package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// An author edit moves the book to a new author directory. Move removes the
// old one once it holds no other books, as Delete does for its parent.
func TestMoveCleansUpTheOldAuthorDir(t *testing.T) {
	for _, tc := range []struct {
		name          string
		sibling       bool
		wantOldAuthor bool
	}{
		{"last book", false, false},
		{"sibling remains", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, root := newStore(t)

			epubName := "Some Title - Wells.epub"
			writeBook(t, root, "Wells, M/Some Title (1)", epubName, "epub-bytes", nil)
			siblingDir := filepath.Join(root, "Wells, M", "Other Title (2)")
			if tc.sibling {
				if err := os.MkdirAll(siblingDir, 0755); err != nil {
					t.Fatal(err)
				}
			}

			from := book.Location{EpubPath: filepath.Join("Wells, M/Some Title (1)", epubName)}
			to := book.Location{EpubPath: filepath.Join("Wells, Martha/Some Title (1)", epubName)}
			if err := s.Move(from, to); err != nil {
				t.Fatalf("Move: %v", err)
			}

			if _, err := os.Stat(filepath.Join(root, to.Dir(), epubName)); err != nil {
				t.Errorf("book not found at new location: %v", err)
			}
			_, err := os.Stat(filepath.Join(root, "Wells, M"))
			if gotOldAuthor := err == nil; gotOldAuthor != tc.wantOldAuthor {
				t.Errorf("old author directory exists = %v, want %v (stat err = %v)", gotOldAuthor, tc.wantOldAuthor, err)
			}
			if tc.sibling {
				if _, err := os.Stat(siblingDir); err != nil {
					t.Errorf("sibling book directory should survive: %v", err)
				}
			}
		})
	}
}

func TestMoveDestinationAlreadyExistsError(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Alice/Book (1)", "Book - Alice.epub", "data", nil)
	writeBook(t, root, "Bob/Book (1)", "", "", nil)

	from := book.Location{EpubPath: filepath.Join("Alice/Book (1)", "Book - Alice.epub")}
	to := book.Location{EpubPath: filepath.Join("Bob/Book (1)", "Book - Bob.epub")}

	err := s.Move(from, to)
	if err == nil {
		t.Fatal("expected error: destination already exists")
	}
	if !strings.Contains(err.Error(), "destination already exists") {
		t.Errorf("error = %q, want 'destination already exists'", err)
	}
}

// Move's compensating rename. The directory move lands first; if the epub
// rename inside it then fails, the directory has to go back where it was,
// otherwise the book sits at a path the index doesn't know, invisible until the
// next drift-triggered rebuild.
func TestMoveRollsBackWhenEpubRenameFails(t *testing.T) {
	s, root := newStore(t)

	// The directory holds an epub under a different name than `from` claims,
	// so the directory move succeeds and the rename inside it cannot.
	writeBook(t, root, "Alice/Title (1)", "actual.epub", "data", &book.Meta{ID: 1})
	from := book.Location{EpubPath: filepath.Join("Alice/Title (1)", "claimed.epub")}
	to := book.Location{EpubPath: filepath.Join("Bob/Title (1)", "renamed.epub")}

	if err := s.Move(from, to); err == nil {
		t.Fatal("Move succeeded with a missing source epub, want the failure surfaced")
	}

	if _, err := os.Stat(filepath.Join(root, from.Dir(), "actual.epub")); err != nil {
		t.Errorf("book did not return to %q after the failed rename: %v", from.Dir(), err)
	}
	if _, err := os.Stat(filepath.Join(root, to.Dir())); !os.IsNotExist(err) {
		t.Errorf("destination %q still exists after rollback", to.Dir())
	}
}

func TestDeleteRemovesBookDir(t *testing.T) {
	s, root := newStore(t)

	dir := filepath.Join(root, "Author, A", "Test (1)")
	writeBook(t, root, "Author, A/Test (1)", "test.epub", "data", nil)

	loc := book.Location{EpubPath: filepath.Join("Author, A/Test (1)", "test.epub")}
	if err := s.Delete(loc); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("book directory should be removed after delete")
	}
	if _, err := os.Stat(filepath.Join(root, "Author, A")); !os.IsNotExist(err) {
		t.Errorf("empty author directory should also be removed after last book is deleted")
	}
}

func TestDeleteWithReadOnlyDirError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	s, root := newStore(t)

	dir := filepath.Join(root, "Author, A", "Test (1)")
	writeBook(t, root, "Author, A/Test (1)", "test.epub", "data", nil)

	if err := os.Chmod(dir, 0444); err != nil {
		t.Skip("cannot chmod book dir:", err)
	}
	// Restore permissions so TempDir cleanup succeeds.
	t.Cleanup(func() { os.Chmod(dir, 0755) })

	loc := book.Location{EpubPath: filepath.Join("Author, A/Test (1)", "test.epub")}
	err := s.Delete(loc)
	if err == nil {
		t.Error("expected error deleting a read-only book directory")
	}
}
