package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Regression test for a production bug: a book ingested before FAT sanitization
// was consistently applied can have an on-disk epub filename containing
// FAT-illegal characters (e.g. ':'), while its directory path (built from the
// raw, unsanitized title) still matches what Layout() recomputes. Move must
// treat this as an in-place filename rename, not a directory move: the
// "destination" directory is the book's own current one, so the old
// "destination already exists" check falsely tripped on every such edit.
func TestMoveSameDirectoryRenamesFilename(t *testing.T) {
	s, root := newStore(t)

	legacyName := "Some Title: With Colon - Some Author.epub"
	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	writeBook(t, root, "Author, Some/Some Title (1)", legacyName, "epub-bytes", &book.Meta{})

	from := book.Location{EpubPath: filepath.Join("Author, Some/Some Title (1)", legacyName)}
	sanitizedName := "Some Title- With Colon - Some Author.epub"
	to := book.Location{EpubPath: filepath.Join("Author, Some/Some Title (1)", sanitizedName)}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, sanitizedName)); err != nil {
		t.Errorf("renamed epub not found: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after rename")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("book directory should be unchanged: %v", err)
	}
}

// Regression test for a live production issue: after moving a book to a new
// author directory (e.g. a title/author edit), the old author directory was left
// behind once it held no other books. Move must clean it up the same way Delete
// already does for its parent.
func TestMoveCleansUpEmptyOldAuthorDir(t *testing.T) {
	s, root := newStore(t)

	epubName := "Some Title - Wells.epub"
	writeBook(t, root, "Wells, M/Some Title (1)", epubName, "epub-bytes", nil)

	from := book.Location{EpubPath: filepath.Join("Wells, M/Some Title (1)", epubName)}
	to := book.Location{EpubPath: filepath.Join("Wells, Martha/Some Title (1)", epubName)}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, to.Dir(), epubName)); err != nil {
		t.Errorf("book not found at new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Wells, M")); !os.IsNotExist(err) {
		t.Errorf("old author directory should have been removed, got err=%v", err)
	}
}

// Move's cleanup only removes the old author directory when it is actually
// empty: sibling books by the same author must survive.
func TestMoveKeepsOldAuthorDirWithRemainingBooks(t *testing.T) {
	s, root := newStore(t)

	epubName := "Some Title - Wells.epub"
	writeBook(t, root, "Wells, M/Some Title (1)", epubName, "epub-bytes", nil)

	siblingDir := filepath.Join(root, "Wells, M", "Other Title (2)")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatal(err)
	}

	from := book.Location{EpubPath: filepath.Join("Wells, M/Some Title (1)", epubName)}
	to := book.Location{EpubPath: filepath.Join("Wells, Martha/Some Title (1)", epubName)}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(siblingDir); err != nil {
		t.Errorf("sibling book directory should survive: %v", err)
	}
}

// The defensive no-op branch: Move must not error or touch the filesystem when
// from and to are identical, even though Edit's guard currently never calls Move
// in that case.
func TestMoveSameLocationNoop(t *testing.T) {
	s, root := newStore(t)

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	writeBook(t, root, "Author, Some/Some Title (1)", "Some Title - Some Author.epub", "epub-bytes", nil)

	loc := book.Location{EpubPath: filepath.Join("Author, Some/Some Title (1)", "Some Title - Some Author.epub")}
	if err := s.Move(loc, loc); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Title - Some Author.epub")); err != nil {
		t.Errorf("epub should be unchanged: %v", err)
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

func TestPathTaken(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author A/Book Title (1)", "Book Title - Author A.epub", "fake epub", nil)

	if !s.PathTaken([]book.Author{{Name: "Author A"}}, "Book Title") {
		t.Error("PathTaken returned false for a file that is on disk")
	}
	if s.PathTaken([]book.Author{{Name: "Author A"}}, "Different Title") {
		t.Error("PathTaken returned true for a title that is not on disk")
	}
	if s.PathTaken([]book.Author{{Name: "Nobody"}}, "Anything") {
		t.Error("PathTaken returned true for a non-existent author dir")
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

// What Library.Edit gets from one Update call: the book lands at the new
// location, the sidecar there carries the meta handed in, and the returned
// observation describes the file at the new path. That last part is load-bearing,
// since the observation goes straight into the index as the book's drift record;
// one describing the pre-move file would make the next startup see drift that
// is not there and rebuild the whole library.
func TestUpdateMovesWritesAndObserves(t *testing.T) {
	s, root := newStore(t)

	const contents = "epub-bytes"
	oldName := "Title - Alice.epub"
	writeBook(t, root, "Alice/Title (1)", oldName, contents, &book.Meta{ID: 1, Status: "unread"})

	from := book.Location{EpubPath: filepath.Join("Alice/Title (1)", oldName)}
	newName := "New Title - Alice & Bob.epub"
	to := book.Location{EpubPath: filepath.Join("Alice & Bob/New Title (1)", newName)}

	pi, err := s.Update(from, to, &book.Meta{ID: 1, Status: "read", Rating: 4})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, to.EpubPath)); err != nil {
		t.Errorf("epub not at the new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Alice")); !os.IsNotExist(err) {
		t.Errorf("old author directory survived an emptying move, err=%v", err)
	}

	meta, err := s.ReadMeta(to)
	if err != nil {
		t.Fatalf("ReadMeta at the new location: %v", err)
	}
	if meta.Status != "read" || meta.Rating != 4 {
		t.Errorf("sidecar = %+v, want the meta Update was handed", meta)
	}

	if pi.IsUnobserved() {
		t.Error("Update returned the unobserved marker for a book it had just written")
	}
	if pi.Size != int64(len(contents)) {
		t.Errorf("observed size = %d, want %d from the file at the new path", pi.Size, len(contents))
	}
}

func TestPathTaken(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author A/Book Title (1)", "Book Title - Author A.epub", "fake epub", nil)

	if !s.PathTaken([]book.Author{{Name: "Author A"}}, "Book Title") {
		t.Error("PathTaken returned false for a file that is on disk")
	}
	if s.PathTaken([]book.Author{{Name: "Author A"}}, "Different Title") {
		t.Error("PathTaken returned true for a title that is not on disk")
	}
	if s.PathTaken([]book.Author{{Name: "Nobody"}}, "Anything") {
		t.Error("PathTaken returned true for a non-existent author dir")
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

// TestUpdateMovesWritesAndObserves pins what Library.Edit gets from one call:
// the book lands at the new location, the sidecar there carries the meta handed
// in, and the returned observation describes the file at the new path. That
// last part is load-bearing — the observation goes straight into the index as
// the book's drift record, so one describing the pre-move file would make the
// next startup see drift that isn't there and rebuild the whole library.
func TestUpdateMovesWritesAndObserves(t *testing.T) {
	s, root := newStore(t)

	const contents = "epub-bytes"
	oldName := "Title - Alice.epub"
	writeBook(t, root, "Alice/Title (1)", oldName, contents, &book.Meta{ID: 1, Status: "unread"})

	from := book.Location{EpubPath: filepath.Join("Alice/Title (1)", oldName)}
	newName := "New Title - Alice & Bob.epub"
	to := book.Location{EpubPath: filepath.Join("Alice & Bob/New Title (1)", newName)}

	pi, err := s.Update(from, to, &book.Meta{ID: 1, Status: "read", Rating: 4})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, to.EpubPath)); err != nil {
		t.Errorf("epub not at the new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Alice")); !os.IsNotExist(err) {
		t.Errorf("old author directory survived an emptying move, err=%v", err)
	}

	meta, err := s.ReadMeta(to)
	if err != nil {
		t.Fatalf("ReadMeta at the new location: %v", err)
	}
	if meta.Status != "read" || meta.Rating != 4 {
		t.Errorf("sidecar = %+v, want the meta Update was handed", meta)
	}

	if pi.IsUnobserved() {
		t.Error("Update returned the unobserved marker for a book it had just written")
	}
	if pi.Size != int64(len(contents)) {
		t.Errorf("observed size = %d, want %d from the file at the new path", pi.Size, len(contents))
	}
}
