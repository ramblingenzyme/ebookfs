package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// TestMoveSameDirectoryRenamesFilename reproduces a production bug: a book
// ingested before FAT sanitization was consistently applied can have an
// on-disk epub filename containing FAT-illegal characters (e.g. ':'), while
// its LibraryPath (built from the raw, unsanitized title) still matches what
// Layout() recomputes. Move must treat this as an in-place filename rename,
// not a directory move — the "destination" directory is the book's own
// current one, so the old "destination already exists" check falsely tripped
// on every such edit.
func TestMoveSameDirectoryRenamesFilename(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	legacyName := "Some Title: With Colon - Some Author.epub"
	if err := os.WriteFile(filepath.Join(dir, legacyName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	oldDir := filepath.Join(root, "Wells, M", "Some Title (1)")
	if err := os.MkdirAll(oldDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubName := "Some Title - Wells.epub"
	if err := os.WriteFile(filepath.Join(oldDir, epubName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	oldAuthorDir := filepath.Join(root, "Wells, M")
	movingDir := filepath.Join(oldAuthorDir, "Some Title (1)")
	if err := os.MkdirAll(movingDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubName := "Some Title - Wells.epub"
	if err := os.WriteFile(filepath.Join(movingDir, epubName), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	siblingDir := filepath.Join(oldAuthorDir, "Other Title (2)")
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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, Some", "Some Title (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Some Title - Some Author.epub"), []byte("epub-bytes"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := model.Location{LibraryPath: "Author, Some/Some Title (1)", EpubFilename: "Some Title - Some Author.epub"}
	if err := s.Move(loc, loc); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "Some Title - Some Author.epub")); err != nil {
		t.Errorf("epub should be unchanged: %v", err)
	}
}

func TestAuthorDirName(t *testing.T) {
	tests := []struct {
		name    string
		authors []model.Author
		want    string
	}{
		{"sort name preferred", []model.Author{{Name: "Alice", SortName: "Smith, Alice"}}, "Smith, Alice"},
		{"name fallback when sort name empty", []model.Author{{Name: "Bob", SortName: ""}}, "Bob"},
		{"name fallback when no sort name", []model.Author{{Name: "Carol"}}, "Carol"},
		{"unknown when no authors", nil, model.UnknownAuthor},
		{"unknown when empty authors", []model.Author{}, model.UnknownAuthor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := authorDirName(tt.authors)
			if got != tt.want {
				t.Errorf("authorDirName(%v) = %q, want %q", tt.authors, got, tt.want)
			}
		})
	}
}

func TestEpubFilename(t *testing.T) {
	tests := []struct {
		name    string
		authors []model.Author
		title   string
		want    string
	}{
		{"with author", []model.Author{{Name: "Alice Author"}}, "Wonderful Title", "Wonderful Title - Alice Author.epub"},
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
		{"basic", []model.Author{{Name: "Alice", SortName: "Smith, Alice"}}, "The Title", 42, "Smith, Alice/The Title (42)"},
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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	loc := s.Layout([]model.Author{{Name: "Alice", SortName: "Smith, Alice"}}, "My Title", 1)
	wantLibPath := "Smith, Alice/My Title (1)"
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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	loc := s.Layout(nil, "Untitled", 99)
	if loc.LibraryPath != "Unknown/Untitled (99)" {
		t.Errorf("LibraryPath = %q, want %q", loc.LibraryPath, "Unknown/Untitled (99)")
	}
	if loc.EpubFilename != "Untitled.epub" {
		t.Errorf("EpubFilename = %q, want %q", loc.EpubFilename, "Untitled.epub")
	}
}

func TestExists(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	// Set up a book directory.
	bookDir := filepath.Join(root, "Author, A", "Book Title (1)")
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubPath := filepath.Join(bookDir, "Book Title - Author A.epub")
	if err := os.WriteFile(epubPath, []byte("fake epub"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Run("book exists", func(t *testing.T) {
		if !s.Exists([]model.Author{{Name: "Author A", SortName: "Author, A"}}, "Book Title") {
			t.Error("Exists returned false, want true")
		}
	})

	t.Run("book does not exist", func(t *testing.T) {
		if s.Exists([]model.Author{{Name: "Author A", SortName: "Author, A"}}, "Different Title") {
			t.Error("Exists returned true, want false")
		}
	})

	t.Run("author dir does not exist", func(t *testing.T) {
		if s.Exists([]model.Author{{Name: "Nobody", SortName: "Nobody"}}, "Anything") {
			t.Error("Exists returned true for non-existent author dir")
		}
	})
}

func TestIngest(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	// Stage a fake epub.
	tmpEpub := filepath.Join(root, ".inbox-tmp", "staged.epub")
	if err := os.MkdirAll(filepath.Dir(tmpEpub), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpEpub, []byte("fake-epub-content"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := s.Layout([]model.Author{{Name: "Alice", SortName: "Smith, Alice"}}, "Ingested", 10)
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
	metaPath := filepath.Join(bookDir, "meta.toml")
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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	// Book 1: Author A / Book One (1)
	b1Dir := filepath.Join(root, "Author, A", "Book One (1)")
	if err := os.MkdirAll(b1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	b1Epub := filepath.Join(b1Dir, "Book One - A.epub")
	if err := os.WriteFile(b1Epub, []byte("book1"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(filepath.Join(b1Dir, "meta.toml"), &model.Meta{ID: 1}); err != nil {
		t.Fatal(err)
	}

	// Book 2: Author A / Book Two (2) — same author
	b2Dir := filepath.Join(root, "Author, A", "Book Two (2)")
	if err := os.MkdirAll(b2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	b2Epub := filepath.Join(b2Dir, "Book Two - A.epub")
	if err := os.WriteFile(b2Epub, []byte("book2"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(filepath.Join(b2Dir, "meta.toml"), &model.Meta{ID: 2}); err != nil {
		t.Fatal(err)
	}

	// Book 3: Author B / Book Three (3) — different author
	b3Dir := filepath.Join(root, "Author, B", "Book Three (3)")
	if err := os.MkdirAll(b3Dir, 0755); err != nil {
		t.Fatal(err)
	}
	b3Epub := filepath.Join(b3Dir, "Book Three - B.epub")
	if err := os.WriteFile(b3Epub, []byte("book3"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(filepath.Join(b3Dir, "meta.toml"), &model.Meta{ID: 3}); err != nil {
		t.Fatal(err)
	}

	// Directory with meta.toml but no epub — should be skipped gracefully.
	staleDir := filepath.Join(root, "Author, C", "Stale (4)")
	if err := os.MkdirAll(staleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := writeMeta(filepath.Join(staleDir, "meta.toml"), &model.Meta{ID: 4}); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	results, err := s.Walk()
	if err != nil {
		t.Fatalf("Walk on empty library: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Walk returned %d entries, want 0", len(results))
	}
}

func TestOpenEpub(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	bookDir := filepath.Join(root, "Author, A", "Test (1)")
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		t.Fatal(err)
	}
	epubPath := filepath.Join(bookDir, "test.epub")
	if err := os.WriteFile(epubPath, []byte("epub-content"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := model.Location{LibraryPath: "Author, A/Test (1)", EpubFilename: "test.epub"}
	f, err := s.OpenEpub(loc)
	if err != nil {
		t.Fatalf("OpenEpub: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 12)
	n, err := f.Read(buf)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(buf[:n]) != "epub-content" {
		t.Errorf("content = %q, want %q", string(buf[:n]), "epub-content")
	}
}

func TestReadMetaRoundTrip(t *testing.T) {
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	bookDir := filepath.Join(root, "Author, A", "Test (1)")
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	// Two authors, each with one book directory.
	aliceDir := filepath.Join(root, "Alice", "Book (1)")
	if err := os.MkdirAll(aliceDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(aliceDir, "Book - Alice.epub"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	bobDir := filepath.Join(root, "Bob", "Book (1)")
	if err := os.MkdirAll(bobDir, 0755); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, A", "Test (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.epub"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

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
	root := t.TempDir()
	s := New(root, filepath.Join(root, ".inbox-tmp"))

	dir := filepath.Join(root, "Author, A", "Test (1)")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.epub"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

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
