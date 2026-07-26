package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// newStore returns a Store rooted at a fresh temp dir, plus that root.
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	return New(root, filepath.Join(root, ".inbox-tmp")), root
}

// writeBook materializes an on-disk book directory under root/libPath: the epub
// (skipped when epubName is empty) and a meta.toml (skipped when meta is nil).
func writeBook(t *testing.T, root, libPath, epubName, content string, meta *model.Meta) {
	t.Helper()
	dir := filepath.Join(root, libPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if epubName != "" {
		if err := os.WriteFile(filepath.Join(dir, epubName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if meta != nil {
		if err := writeMeta(filepath.Join(dir, metaFilename), meta); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMoveSameDirectoryRenamesFilename reproduces a production bug: a book
// ingested before FAT sanitization was consistently applied can have an
// on-disk epub filename containing FAT-illegal characters (e.g. ':'), while
// its LibraryPath (built from the raw, unsanitized title) still matches what
// Layout() recomputes. Move must treat this as an in-place filename rename,
// not a directory move — the "destination" directory is the book's own
// current one, so the old "destination already exists" check falsely tripped
// on every such edit.
func TestMoveSameDirectoryRenamesFilename(t *testing.T) {
	s, root := newStore(t)

	legacyName := "Some Title: With Colon - Some Author.epub"
	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	writeBook(t, root, "Author, Some/Some Title (1)", legacyName, "epub-bytes", &model.Meta{})

	from := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: legacyName}
	sanitizedName := "Some Title- With Colon - Some Author.epub"
	to := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: sanitizedName}

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

// TestMoveCleansUpEmptyOldAuthorDir reproduces a live production issue: after
// moving a book to a new author directory (e.g. a title/author edit), the old
// author directory was left behind once it held no other books. Move must
// clean it up the same way Delete already does for its parent.
func TestMoveCleansUpEmptyOldAuthorDir(t *testing.T) {
	s, root := newStore(t)

	epubName := "Some Title - Wells.epub"
	writeBook(t, root, "Wells, M/Some Title (1)", epubName, "epub-bytes", nil)

	from := model.Location{LibraryPath: "Wells, M/Some Title (1)", EpubFilename: epubName}
	to := model.Location{LibraryPath: "Wells, Martha/Some Title (1)", EpubFilename: epubName}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(filepath.Join(root, "Wells, Martha", "Some Title (1)", epubName)); err != nil {
		t.Errorf("book not found at new location: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "Wells, M")); !os.IsNotExist(err) {
		t.Errorf("old author directory should have been removed, got err=%v", err)
	}
}

// TestMoveKeepsOldAuthorDirWithRemainingBooks ensures Move's cleanup only
// removes the old author directory when it's actually empty — sibling books
// by the same author must survive.
func TestMoveKeepsOldAuthorDirWithRemainingBooks(t *testing.T) {
	s, root := newStore(t)

	epubName := "Some Title - Wells.epub"
	writeBook(t, root, "Wells, M/Some Title (1)", epubName, "epub-bytes", nil)

	siblingDir := filepath.Join(root, "Wells, M", "Other Title (2)")
	if err := os.MkdirAll(siblingDir, 0755); err != nil {
		t.Fatal(err)
	}

	from := model.Location{LibraryPath: "Wells, M/Some Title (1)", EpubFilename: epubName}
	to := model.Location{LibraryPath: "Wells, Martha/Some Title (1)", EpubFilename: epubName}

	if err := s.Move(from, to); err != nil {
		t.Fatalf("Move: %v", err)
	}

	if _, err := os.Stat(siblingDir); err != nil {
		t.Errorf("sibling book directory should survive: %v", err)
	}
}

// TestMoveSameLocationNoop covers the defensive no-op branch: Move must not
// error or touch the filesystem when from and to are identical, even though
// Edit's guard currently never calls Move in that case.
func TestMoveSameLocationNoop(t *testing.T) {
	s, root := newStore(t)

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	writeBook(t, root, "Author, Some/Some Title (1)", "Some Title - Some Author.epub", "epub-bytes", nil)

	loc := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: "Some Title - Some Author.epub"}
	if err := s.Move(loc, loc); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Title - Some Author.epub")); err != nil {
		t.Errorf("epub should be unchanged: %v", err)
	}
}

func TestEpubFilename(t *testing.T) {
	tests := []struct {
		name    string
		authors []model.Author
		title   string
		want    string
	}{
		{"single author", []model.Author{{Name: "Alice"}}, "Wonderful Title", "Wonderful Title - Alice.epub"},
		{"two authors", []model.Author{{Name: "Alice"}, {Name: "Bob"}}, "Title", "Title - Alice & Bob.epub"},
		{"no authors", nil, "No Author Book", "No Author Book.epub"},
		{"empty authors", []model.Author{}, "Empty Authors", "Empty Authors.epub"},
		{"colon in title", []model.Author{{Name: "Alice"}}, "Title: Sub", "Title- Sub - Alice.epub"},
		{"slash in author", []model.Author{{Name: "Alice/Author"}}, "Title", "Title - Alice-Author.epub"},
		{"leading dot trimmed", []model.Author{{Name: "Alice"}}, ".hidden", "hidden - Alice.epub"},
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

func TestCanonicalDir(t *testing.T) {
	tests := []struct {
		name    string
		authors []model.Author
		title   string
		id      int64
		want    string
	}{
		{"basic", []model.Author{{Name: "Alice"}}, "The Title", 42, "Alice/The Title (42)"},
		{"two authors", []model.Author{{Name: "Alice"}, {Name: "Bob"}}, "The Title", 42, "Alice & Bob/The Title (42)"},
		{"unknown author", nil, "No Author", 1, "Unknown/No Author (1)"},
		{"title with id", []model.Author{{Name: "Bob"}}, "My Book", 7, "Bob/My Book (7)"},
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

func TestLayout(t *testing.T) {
	s, root := newStore(t)

	loc := s.Layout([]model.Author{{Name: "Alice"}}, "My Title", 1)
	wantLibPath := "Alice/My Title (1)"
	wantEpub := "My Title - Alice.epub"
	if loc.LibraryPath != wantLibPath {
		t.Errorf("LibraryPath = %q, want %q", loc.LibraryPath, wantLibPath)
	}
	if loc.EpubFilename != wantEpub {
		t.Errorf("EpubFilename = %q, want %q", loc.EpubFilename, wantEpub)
	}
	if loc.EpubPath != filepath.Join(root, wantLibPath, wantEpub) {
		t.Errorf("EpubPath = %q, want %q", loc.EpubPath, filepath.Join(root, wantLibPath, wantEpub))
	}
}

func TestLayoutUnknownAuthor(t *testing.T) {
	s, _ := newStore(t)

	loc := s.Layout(nil, "Untitled", 99)
	if loc.LibraryPath != "Unknown/Untitled (99)" {
		t.Errorf("LibraryPath = %q, want %q", loc.LibraryPath, "Unknown/Untitled (99)")
	}
	if loc.EpubFilename != "Untitled.epub" {
		t.Errorf("EpubFilename = %q, want %q", loc.EpubFilename, "Untitled.epub")
	}
}

func TestExists(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author A/Book Title (1)", "Book Title - Author A.epub", "fake epub", nil)

	t.Run("book exists", func(t *testing.T) {
		if !s.Exists([]model.Author{{Name: "Author A"}}, "Book Title") {
			t.Error("Exists returned false, want true")
		}
	})

	t.Run("book does not exist", func(t *testing.T) {
		if s.Exists([]model.Author{{Name: "Author A"}}, "Different Title") {
			t.Error("Exists returned true, want false")
		}
	})

	t.Run("author dir does not exist", func(t *testing.T) {
		if s.Exists([]model.Author{{Name: "Nobody"}}, "Anything") {
			t.Error("Exists returned true for non-existent author dir")
		}
	})
}

func TestIngest(t *testing.T) {
	s, root := newStore(t)

	// Stage a fake epub.
	tmpEpub := filepath.Join(root, ".inbox-tmp", "staged.epub")
	if err := os.MkdirAll(filepath.Dir(tmpEpub), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpEpub, []byte("fake-epub-content"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := s.Layout([]model.Author{{Name: "Alice"}}, "Ingested", 10)
	meta := &model.Meta{ID: 10}

	if err := s.Ingest(tmpEpub, loc, meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	// Verify the epub landed at the right place.
	bookDir := filepath.Join(root, loc.LibraryPath)
	if _, err := os.Stat(bookDir); err != nil {
		t.Errorf("book directory not created: %v", err)
	}
	gotEpub := filepath.Join(bookDir, loc.EpubFilename)
	if _, err := os.Stat(gotEpub); err != nil {
		t.Errorf("epub not found at %s: %v", gotEpub, err)
	}
	data, err := os.ReadFile(gotEpub)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "fake-epub-content" {
		t.Errorf("epub content = %q, want %q", string(data), "fake-epub-content")
	}

	// Verify meta.toml was written.
	metaPath := filepath.Join(bookDir, metaFilename)
	if _, err := os.Stat(metaPath); err != nil {
		t.Errorf("meta.toml not found: %v", err)
	}
	gotMeta, err := readMeta(metaPath)
	if err != nil {
		t.Fatalf("readMeta: %v", err)
	}
	if gotMeta.ID != 10 {
		t.Errorf("meta ID = %d, want %d", gotMeta.ID, 10)
	}
}

func TestWalk(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author, A/Book One (1)", "Book One - A.epub", "book1", &model.Meta{ID: 1})
	// Same author as Book One.
	writeBook(t, root, "Author, A/Book Two (2)", "Book Two - A.epub", "book2", &model.Meta{ID: 2})
	// Different author.
	writeBook(t, root, "Author, B/Book Three (3)", "Book Three - B.epub", "book3", &model.Meta{ID: 3})
	// Directory with meta.toml but no epub — should be skipped gracefully.
	writeBook(t, root, "Author, C/Stale (4)", "", "", &model.Meta{ID: 4})

	results, err := s.Walk()
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Walk returned %d entries, want 3", len(results))
	}

	found := make(map[string]bool)
	for _, loc := range results {
		found[loc.LibraryPath] = true
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

func TestReadMetaRoundTrip(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author, A/Test (1)", "", "", nil)

	loc := model.Location{LibraryPath: "Author, A/Test (1)", EpubFilename: "test.epub"}
	original := &model.Meta{
		ID:     42,
		Status: "reading",
		Rating: 3.5,
		Tags:   []string{"sci-fi", "classic"},
	}

	if err := s.WriteMeta(loc, original); err != nil {
		t.Fatalf("WriteMeta: %v", err)
	}

	got, err := s.ReadMeta(loc)
	if err != nil {
		t.Fatalf("ReadMeta: %v", err)
	}

	if got.ID != original.ID {
		t.Errorf("ID = %d, want %d", got.ID, original.ID)
	}
	if got.Status != original.Status {
		t.Errorf("Status = %q, want %q", got.Status, original.Status)
	}
	if got.Rating != original.Rating {
		t.Errorf("Rating = %g, want %g", got.Rating, original.Rating)
	}
	if len(got.Tags) != len(original.Tags) || got.Tags[0] != original.Tags[0] {
		t.Errorf("Tags = %v, want %v", got.Tags, original.Tags)
	}
}

func TestMoveDestinationAlreadyExistsError(t *testing.T) {
	s, root := newStore(t)

	// Two authors, each with one book directory (Bob's has no epub of its own).
	writeBook(t, root, "Alice/Book (1)", "Book - Alice.epub", "data", nil)
	writeBook(t, root, "Bob/Book (1)", "", "", nil)

	from := model.Location{LibraryPath: "Alice/Book (1)", EpubFilename: "Book - Alice.epub"}
	to := model.Location{LibraryPath: "Bob/Book (1)", EpubFilename: "Book - Bob.epub"}

	err := s.Move(from, to)
	if err == nil {
		t.Fatal("expected error: destination already exists")
	}
	if !strings.Contains(err.Error(), "destination already exists") {
		t.Errorf("error = %q, want 'destination already exists'", err)
	}
}

func TestReadMetaNotExist(t *testing.T) {
	_, err := readMeta("/nonexistent/path/meta.toml")
	if err == nil {
		t.Error("expected error reading non-existent meta.toml")
	}
}

func TestReadMetaInvalidTOML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meta.toml")
	if err := os.WriteFile(path, []byte("invalid toml {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := readMeta(path)
	if err == nil {
		t.Error("expected error for invalid TOML in meta.toml")
	}
}

func TestWriteMetaReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0444); err != nil {
		t.Skip("cannot chmod temp dir:", err)
	}
	path := filepath.Join(dir, "meta.toml")
	err := writeMeta(path, &model.Meta{ID: 1})
	if err == nil {
		t.Error("expected error writing meta.toml to read-only directory")
	}
}

func TestDeleteRemovesBookDir(t *testing.T) {
	s, root := newStore(t)

	dir := filepath.Join(root, "Author, A", "Test (1)")
	writeBook(t, root, "Author, A/Test (1)", "test.epub", "data", nil)

	loc := model.Location{LibraryPath: "Author, A/Test (1)", EpubFilename: "test.epub"}
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

func TestEpubFilenameForFFallback(t *testing.T) {
	// A title that is only dots triggers ForFAT to return an error (trimmed to empty),
	// exercising the fallback to the raw title.
	got := epubFilename([]model.Author{{Name: "Alice"}}, ".")
	if got != ". - Alice.epub" {
		t.Errorf("epubFilename = %q, want %q", got, ". - Alice.epub")
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

	loc := model.Location{LibraryPath: "Author, A/Test (1)", EpubFilename: "test.epub"}
	err := s.Delete(loc)
	if err == nil {
		t.Error("expected error deleting a read-only book directory")
	}
}

// TestIDFromPath pins the inverse of canonicalDir's " (id)" suffix. It is the
// only way to recover a book's id when meta.toml can't be parsed, so it has to
// reject anything it isn't certain about rather than guess.
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

// TestIngestSurfacesWriteMetaFailure verifies that Ingest returns an error
// when the sidecar write fails, but does NOT clean up — the caller is
// responsible for deciding whether to delete the partial directory.
func TestIngestSurfacesWriteMetaFailure(t *testing.T) {
	s, root := newStore(t)

	staged := filepath.Join(t.TempDir(), "staged.epub")
	if err := os.WriteFile(staged, []byte("epub"), 0644); err != nil {
		t.Fatal(err)
	}
	loc := model.Location{LibraryPath: "Alice/Title (1)", EpubFilename: "Title - Alice.epub"}
	// Occupy meta.toml's name with a directory, so the sidecar write fails
	// after the epub is already in place.
	bookDir := filepath.Join(root, loc.LibraryPath)
	if err := os.MkdirAll(filepath.Join(bookDir, metaFilename), 0755); err != nil {
		t.Fatal(err)
	}

	if err := s.Ingest(staged, loc, &model.Meta{ID: 1}); err == nil {
		t.Fatal("Ingest succeeded with an unwritable meta.toml, want the failure surfaced")
	}
	if _, err := os.Stat(bookDir); os.IsNotExist(err) {
		t.Error("Ingest cleaned up the book directory; caller is responsible for cleanup")
	}
}

// TestIngestMissingStagedEpub is the earlier failure: nothing to move into
// place. The book directory is created before the rename is attempted, so this
// pins that the error is surfaced rather than leaving a half-built book behind.
func TestIngestMissingStagedEpub(t *testing.T) {
	s, root := newStore(t)

	loc := model.Location{LibraryPath: "Alice/Title (1)", EpubFilename: "Title - Alice.epub"}
	err := s.Ingest(filepath.Join(t.TempDir(), "does-not-exist.epub"), loc, &model.Meta{ID: 1})
	if err == nil {
		t.Fatal("Ingest succeeded with no staged epub, want the failure surfaced")
	}
	if _, err := os.Stat(filepath.Join(root, loc.LibraryPath, metaFilename)); !os.IsNotExist(err) {
		t.Error("meta.toml was written despite the epub never arriving")
	}
}

// TestMoveRollsBackWhenEpubRenameFails covers Move's compensating rename. The
// directory move lands first; if the epub rename inside it then fails, the
// directory has to go back where it was — otherwise the book sits at a path
// the index doesn't know, invisible until the next drift-triggered rebuild.
func TestMoveRollsBackWhenEpubRenameFails(t *testing.T) {
	s, root := newStore(t)

	// The directory holds an epub under a different name than `from` claims,
	// so the directory move succeeds and the rename inside it cannot.
	writeBook(t, root, "Alice/Title (1)", "actual.epub", "data", &model.Meta{ID: 1})
	from := model.Location{LibraryPath: "Alice/Title (1)", EpubFilename: "claimed.epub"}
	to := model.Location{LibraryPath: "Bob/Title (1)", EpubFilename: "renamed.epub"}

	if err := s.Move(from, to); err == nil {
		t.Fatal("Move succeeded with a missing source epub, want the failure surfaced")
	}

	if _, err := os.Stat(filepath.Join(root, from.LibraryPath, "actual.epub")); err != nil {
		t.Errorf("book did not return to %q after the failed rename: %v", from.LibraryPath, err)
	}
	if _, err := os.Stat(filepath.Join(root, to.LibraryPath)); !os.IsNotExist(err) {
		t.Errorf("destination %q still exists after rollback", to.LibraryPath)
	}
}
