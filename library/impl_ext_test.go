package library_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"
)

// TestClose asserts the returned error, which is the index's. openTestLibrary
// also registers a Close via t.Cleanup, so these tests double as coverage of
// closing an already-closed library.
func TestClose(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// TestCloseMultiple pins that closing twice is safe and still reports success —
// the 9P server closes on shutdown and t.Cleanup closes again, so a second
// close returning an error would turn every test teardown into a failure.
func TestCloseMultiple(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := lib.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestGetReturnsTheIngestedBook(t *testing.T) {
	lib := openTestLibrary(t)
	want := ingestTestEpub(t, lib, buildTestEpub(t, "Fetched By Id"))

	got, err := lib.Get(want.ID())
	if err != nil {
		t.Fatalf("Get(%d): %v", want.ID(), err)
	}
	if got.ID() != want.ID() || got.Title() != want.Title() {
		t.Errorf("Get = %d/%q, want %d/%q", got.ID(), got.Title(), want.ID(), want.Title())
	}
}

// Every id-addressed read reports a missing book the same way, so a caller can
// test one sentinel whichever it called.
func TestGetMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	if _, err := lib.Content(999); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Content(999) err = %v, want ErrBookNotFound", err)
	}
	if _, err := lib.Get(999); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Get(999) err = %v, want ErrBookNotFound", err)
	}
}

// TestLibraryImplSearchHydratesEpubPath covers the half of Search the index
// cannot do. The index stores a library-relative path; every consumer of a
// result — the exporter, the 9P epub file — needs an absolute one, so Search
// fills it in on the way out. Query has the same loop and its own coverage;
// Search's copy had none, so deleting it broke no test while leaving every
// search result unopenable.
func TestLibraryImplSearchHydratesEpubPath(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	want := ingestTestEpub(t, lib, buildTestEpub(t, "Findable", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Unrelated", "Bob"))

	got, err := lib.Search(library.Query{Titles: []string{"Findable"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Search returned %d books, want just the matching one", len(got))
	}
	if got[0].ID() != want.ID() {
		t.Errorf("id = %d, want %d", got[0].ID(), want.ID())
	}
	if got[0].EpubPath() == "" {
		t.Fatal("EpubPath is empty — the result carries no path, so nothing can open it")
	}
	if filepath.IsAbs(got[0].EpubPath()) {
		t.Errorf("EpubPath = %q, want a relative path", got[0].EpubPath())
	}
	absPath := filepath.Join(cfg.Root, got[0].EpubPath())
	if _, err := os.Stat(absPath); err != nil {
		t.Errorf("EpubPath %q does not resolve: %v", got[0].EpubPath(), err)
	}
}

// TestLibraryImplSearchNoMatches pins that a query matching nothing is not an
// error — the 9P search directory reads an empty result as "no books", and an
// error there would surface as a failed readdir instead.
func TestLibraryImplSearchNoMatches(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Findable", "Alice"))

	got, err := lib.Search(library.Query{Titles: []string{"nothing matches this"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search returned %d books, want none", len(got))
	}
}

func TestLibraryImplStats(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Stats Test"))

	s, err := lib.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Books != 1 {
		t.Errorf("Books = %d, want 1", s.Books)
	}
}

func TestLibraryContentReadsEpub(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Open Read"))
	id := book.ID()

	r, err := lib.Content(id)
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	defer r.Close()

	data, err := io.ReadAll(io.NewSectionReader(r, 0, 1<<20))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(data) == 0 {
		t.Error("epub content should not be empty")
	}
}

func TestLibraryContentNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	_, err := lib.Content(99999)
	if err == nil {
		t.Fatal("expected error for non-existent book")
	}
}

func TestLibraryImplCover(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Cover Test"))
	id := book.ID()

	r, err := lib.Content(id)
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	defer r.Close()
	data, err := r.Cover()
	if err != nil {
		t.Fatalf("Cover: %v", err)
	}
	if string(data) != "placeholder-cover-bytes" {
		t.Errorf("cover data = %q, want %q", string(data), "placeholder-cover-bytes")
	}
}

func TestLibraryImplExtractOPF(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "OPF Title"))
	id := book.ID()

	r, err := lib.Content(id)
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	defer r.Close()
	data, err := r.OPF()
	if err != nil {
		t.Fatalf("OPF: %v", err)
	}
	if !strings.Contains(string(data), "OPF Title") {
		t.Errorf("OPF should contain title, got: %s", string(data))
	}
}

// TestEditSelfHealsLegacyUnsanitizedFilename reproduces a production bug: a
// book ingested before FAT sanitization was consistently applied has an
// on-disk epub filename containing FAT-illegal characters (e.g. ':'), while
// its LibraryPath (built from the raw, unsanitized title) still matches what
// Layout() recomputes. An Edit() that only touches an unrelated field
// (Status) leaves Title/Authors unchanged, so Layout() recomputes the same
// LibraryPath but a different (sanitized) EpubFilename, and Move() must
// handle this same-directory rename rather than fail on "destination already
// exists".
func TestEditSelfHealsLegacyUnsanitizedFilename(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	title := "Some Title: With Colon"
	book := ingestTestEpub(t, lib, buildTestEpub(t, title))
	id := book.ID()

	// Simulate legacy drift: rename the on-disk epub to an unsanitized name
	// that Layout() would no longer produce, then reindex so the index's
	// stored EpubFilename reflects the manual rename.
	root := cfg.Root
	dir := filepath.Join(root, filepath.Dir(book.EpubPath()))
	absEpub := filepath.Join(root, book.EpubPath())
	legacyName := "Some Title: With Colon - Alice.epub"
	if err := os.Rename(absEpub, filepath.Join(dir, legacyName)); err != nil {
		t.Fatal(err)
	}
	if err := lib.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Edit an unrelated field (Status) — Title/Authors untouched. This used
	// to hit "destination already exists" and fail every time.
	status := "reading"
	updated, err := lib.Edit(id, library.Edits{Status: &status})
	if err != nil {
		t.Fatalf("Edit: %v (same-directory Move must not fail)", err)
	}
	if updated.Status() != status {
		t.Errorf("status = %q, want %q", updated.Status(), status)
	}

	// Self-heal: filename should now be FAT-sanitized.
	if strings.Contains(filepath.Base(updated.EpubPath()), ":") {
		t.Errorf("filename not sanitized after edit: %q", filepath.Base(updated.EpubPath()))
	}
	if _, err := os.Stat(filepath.Join(root, updated.EpubPath())); err != nil {
		t.Errorf("updated EpubPath does not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after self-heal")
	}

	// A second edit must also succeed (regression: the original bug caused
	// every subsequent edit to fail identically, not just the first).
	status2 := "read"
	if _, err := lib.Edit(id, library.Edits{Status: &status2}); err != nil {
		t.Fatalf("second Edit: %v", err)
	}
}

func TestDeleteRemovesBook(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "To Delete"))
	id := book.ID()

	if err := lib.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Book should no longer be queryable.
	results, err := lib.Search(library.Query{IDs: []int64{id}})
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query returned %d results after delete, want 0", len(results))
	}
}

func TestDeleteRemovesOnDisk(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Delete On Disk"))
	id := book.ID()

	if err := lib.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// The epub file must no longer exist on disk.
	if _, err := os.Stat(book.EpubPath()); !os.IsNotExist(err) {
		t.Errorf("epub should be removed after delete, stat err = %v", err)
	}
}

func TestDeleteNonexistentBookErrors(t *testing.T) {
	lib := openTestLibrary(t)
	err := lib.Delete(9999)
	if err == nil {
		t.Fatal("expected error when deleting a non-existent book")
	}
	if !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("error = %v, want ErrBookNotFound", err)
	}
}

// TestEditMissingBookIsErrBookNotFound completes the error-identity contract.
// Get, Content and Delete already assert it; Edit is the remaining mutation
// that addresses a book by id, and a frontend distinguishes "no such book"
// from an index failure only through errors.Is.
func TestEditMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	if _, err := lib.Edit(9999, library.Edits{Status: new("read")}); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Edit(9999) err = %v, want ErrBookNotFound", err)
	}
}

// TestSearchSnapshotsAreImmutable pins the concurrency contract stated on
// Library: a *Book handed out is a snapshot the library never mutates. The 9P
// tree relies on it — BookDir holds one of these behind an atomic pointer and
// reads it from many goroutines with no lock — so a library that edited a
// returned Book in place would tear values under those readers.
func TestSearchSnapshotsAreImmutable(t *testing.T) {
	lib := openTestLibrary(t)
	ingestTestEpub(t, lib, buildTestEpub(t, "Before", "Alice"))

	got, err := lib.Search(library.Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	held := got[0]

	if _, err := lib.Edit(held.ID(), library.Edits{Title: new("After")}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if held.Title() != "Before" {
		t.Errorf("the held snapshot changed to %q; the library mutated a Book it had handed out", held.Title())
	}

	// The getters copy too, so a caller cannot reach back through one.
	authors := held.Authors()
	authors[0].Name = "Mallory"
	if held.Authors()[0].Name != "Alice" {
		t.Error("Authors() handed out the book's own slice; mutating it changed the snapshot")
	}
	tags := held.Tags()
	tags = append(tags, "injected")
	_ = tags
	if len(held.Tags()) != 0 {
		t.Error("Tags() handed out the book's own slice")
	}
}
