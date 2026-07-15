package library

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// writeManualBookDir lays down a book directory storeDrifted (via store.Walk)
// will discover, without going through the library's ingest path — simulating
// a book added directly to the store on disk. Walk only checks for meta.toml's
// presence and an *.epub file, so their contents don't need to be valid.
func writeManualBookDir(t *testing.T, lib Library, libraryPath string) {
	t.Helper()
	l := lib.(*libraryImpl)
	dir := filepath.Dir(l.store.AbsPath(libraryPath, "meta.toml"))
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meta.toml"), []byte("id = 999\n"), 0644); err != nil {
		t.Fatalf("write meta.toml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "book.epub"), []byte("manual"), 0644); err != nil {
		t.Fatalf("write book.epub: %v", err)
	}
}

func TestStoreDriftedCleanIndex(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Clean"))

	if lib.(*libraryImpl).storeDrifted() {
		t.Error("storeDrifted() = true, want false for an index that matches the store")
	}
}

func TestStoreDriftedDetectsAddedBook(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

	writeManualBookDir(t, lib, "Manual/Added Book (999)")

	if !lib.(*libraryImpl).storeDrifted() {
		t.Error("storeDrifted() = false, want true after a book directory was added by hand")
	}
}

func TestStoreDriftedDetectsRemovedBook(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Doomed"))

	if err := os.RemoveAll(filepath.Dir(book.EpubPath)); err != nil {
		t.Fatalf("remove book dir: %v", err)
	}

	if !lib.(*libraryImpl).storeDrifted() {
		t.Error("storeDrifted() = false, want true after a book directory was removed by hand")
	}
}

func TestStoreDriftedDetectsSwappedEpub(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Original"))

	swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
	if err := os.WriteFile(book.EpubPath, swapped, 0644); err != nil {
		t.Fatalf("swap epub: %v", err)
	}

	if !lib.(*libraryImpl).storeDrifted() {
		t.Error("storeDrifted() = false, want true after the epub file was swapped by hand")
	}
}

// TestOpenReindexesOnDrift is the end-to-end case: a manual edit to the store
// while the server is down must be picked up on the next plain restart (no
// -reindex flag), because Open consults storeDrifted alongside needsReindex.
func TestOpenReindexesOnDrift(t *testing.T) {
	dir := t.TempDir()
	cfg := config.LibraryConfig{
		Root:      filepath.Join(dir, "root"),
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}

	lib, err := Open(cfg, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Before"))
	id := book.Meta.ID
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	swapped := buildTestEpub(t, "A Completely Different And Much Longer Title")
	if err := os.WriteFile(book.EpubPath, swapped, 0644); err != nil {
		t.Fatalf("swap epub while server is down: %v", err)
	}

	lib2, err := Open(cfg, false) // plain restart, no -reindex
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer lib2.Close()

	got, err := lib2.Query(model.Filter{ID: id})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	want := "A Completely Different And Much Longer Title"
	if got[0].Title != want {
		t.Errorf("Title = %q, want %q (drift not picked up on restart)", got[0].Title, want)
	}
}

// TestReindexMigratesToCanonicalPath verifies that the Layout/Move pass during
// reindex relocates books from old-style paths (single-author directories,
// sort-name directories) to the canonical naming convention. The test ingests
// a multi-author book, moves it to a single-author directory on disk, then
// triggers a reindex and confirms the book is restored to the "Alice & Bob"
// directory with the all-author epub filename.
func TestReindexMigratesToCanonicalPath(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	cfg := config.LibraryConfig{
		Root:      root,
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}

	lib, err := Open(cfg, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Test Title", "Alice", "Bob"))
	canonicalPath := book.Location.LibraryPath   // e.g. "Alice & Bob/Test Title (1)"
	canonicalEpub := book.Location.EpubFilename  // e.g. "Test Title - Alice & Bob.epub"
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Move the book directory from the multi-author path to a single-author
	// path on disk, simulating an old-style layout or external interference.
	oldDir := filepath.Join(root, "Alice", fmt.Sprintf("Test Title (%d)", book.Meta.ID))
	if err := os.MkdirAll(filepath.Dir(oldDir), 0755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, canonicalPath), oldDir); err != nil {
		t.Fatalf("rename book dir to old path: %v", err)
	}
	// The epub filename also needs the old single-author form.
	oldEpub := "Test Title - Alice.epub"
	if err := os.Rename(filepath.Join(oldDir, canonicalEpub), filepath.Join(oldDir, oldEpub)); err != nil {
		t.Fatalf("rename epub to old filename: %v", err)
	}

	// Reopen with force reindex — triggers Layout/Move.
	lib2, err := Open(cfg, true)
	if err != nil {
		t.Fatalf("Open (reindex): %v", err)
	}
	defer lib2.Close()

	got, err := lib2.Query(model.Filter{ID: book.Meta.ID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1", len(got))
	}
	if got[0].Location.LibraryPath != canonicalPath {
		t.Errorf("LibraryPath = %q, want %q", got[0].Location.LibraryPath, canonicalPath)
	}
	if got[0].Location.EpubFilename != canonicalEpub {
		t.Errorf("EpubFilename = %q, want %q", got[0].Location.EpubFilename, canonicalEpub)
	}

	// The canonical directory should exist on disk.
	if _, err := os.Stat(filepath.Join(root, canonicalPath)); err != nil {
		t.Errorf("canonical dir missing after reindex: %v", err)
	}
}

// TestReindexMigratesFromSortNamePath verifies that a book originally placed
// at a SortName-based path (e.g. "Smith, Alice/Title (id)/") is relocated to
// the display-name path ("Alice/Title (id)/") during reindex. The epub filename
// is unaffected — it always used display name, never SortName.
func TestReindexMigratesFromSortNamePath(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "root")
	cfg := config.LibraryConfig{
		Root:      root,
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}

	lib, err := Open(cfg, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	book := ingestTestEpub(t, lib, buildTestEpub(t, "The Title", "Alice"))
	canonicalPath := book.Location.LibraryPath   // "Alice/The Title (1)"
	canonicalEpub := book.Location.EpubFilename  // "The Title - Alice.epub"
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Rename directory to old SortName-based path (Last, First).
	oldDir := filepath.Join(root, "Smith, Alice", fmt.Sprintf("The Title (%d)", book.Meta.ID))
	if err := os.MkdirAll(filepath.Dir(oldDir), 0755); err != nil {
		t.Fatalf("mkdir old dir: %v", err)
	}
	if err := os.Rename(filepath.Join(root, canonicalPath), oldDir); err != nil {
		t.Fatalf("rename to SortName path: %v", err)
	}
	// Epub filename stays the same (it always used display Name, not SortName).

	lib2, err := Open(cfg, true)
	if err != nil {
		t.Fatalf("Open (reindex): %v", err)
	}
	defer lib2.Close()

	got, err := lib2.Query(model.Filter{ID: book.Meta.ID})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d books, want 1", len(got))
	}
	if got[0].Location.LibraryPath != canonicalPath {
		t.Errorf("LibraryPath = %q, want %q", got[0].Location.LibraryPath, canonicalPath)
	}
	if got[0].Location.EpubFilename != canonicalEpub {
		t.Errorf("EpubFilename = %q, want %q", got[0].Location.EpubFilename, canonicalEpub)
	}
	if _, err := os.Stat(filepath.Join(root, canonicalPath)); err != nil {
		t.Errorf("canonical dir missing after reindex: %v", err)
	}
}
