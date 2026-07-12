package library

import (
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
