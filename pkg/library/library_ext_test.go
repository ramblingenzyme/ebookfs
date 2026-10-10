package library_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestClose(t *testing.T) {
	lib := openTestLibrary(t)

	if err := lib.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Shutdown and t.Cleanup both close, so a second close must succeed.
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

func TestGetMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	if _, err := lib.Content(999); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Content(999) err = %v, want ErrBookNotFound", err)
	}
	if _, err := lib.Get(999); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Get(999) err = %v, want ErrBookNotFound", err)
	}
}

// Search returns the path relative to the store root, and every consumer joins
// it to the root. An empty or absolute path would leave the result unopenable.
func TestSearchReturnsARootRelativeEpubPath(t *testing.T) {
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

// The 9P search view reads an empty result as no books; an error would fail the
// readdir.
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

// Regression: a book filed before FAT sanitization has a ':' in its epub name,
// under a directory Layout still produces. A status-only edit must rename it in
// place, not fail on "destination already exists".
func TestEditSelfHealsLegacyUnsanitizedFilename(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	title := "Some Title: With Colon"
	book := ingestTestEpub(t, lib, buildTestEpub(t, title))
	id := book.ID()

	// Rename the on-disk epub to a name Layout would no longer produce, then
	// reindex so the index records the manual rename.
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

	// An unrelated field, so Layout keeps the directory.
	status := "reading"
	updated, err := lib.Edit(id, library.Edits{Status: &status})
	if err != nil {
		t.Fatalf("Edit: %v (same-directory Move must not fail)", err)
	}
	if updated.Status() != status {
		t.Errorf("status = %q, want %q", updated.Status(), status)
	}

	if strings.Contains(filepath.Base(updated.EpubPath()), ":") {
		t.Errorf("filename not sanitized after edit: %q", filepath.Base(updated.EpubPath()))
	}
	if _, err := os.Stat(filepath.Join(root, updated.EpubPath())); err != nil {
		t.Errorf("updated EpubPath does not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after self-heal")
	}

	// The bug failed every later edit too.
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

	results, err := lib.Search(library.Query{IDs: []int64{id}})
	if err != nil {
		t.Fatalf("Query after delete: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Query returned %d results after delete, want 0", len(results))
	}
}

func TestDeleteRemovesOnDisk(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Delete On Disk"))
	dir := filepath.Join(cfg.Root, filepath.Dir(book.EpubPath()))
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("book directory missing before delete: %v", err)
	}

	if err := lib.Delete(book.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("book directory should be removed after delete, stat err = %v", err)
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

func TestEditMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	if _, err := lib.Edit(9999, library.Edits{Status: new("read")}); !errors.Is(err, library.ErrBookNotFound) {
		t.Errorf("Edit(9999) err = %v, want ErrBookNotFound", err)
	}
}

// BookDir reads a *Book from many goroutines without a lock, so the library
// must never mutate one it handed out.
func TestSearchSnapshotsAreImmutable(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Before", "Alice"))
	if _, err := lib.Edit(b.ID(), library.Edits{
		Tags:        &[]string{"kept"},
		Series:      new("Saga"),
		SeriesIndex: new("1"),
	}); err != nil {
		t.Fatalf("Edit tags and series: %v", err)
	}

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
	tags[0] = "injected"
	if held.Tags()[0] != "kept" {
		t.Error("Tags() handed out the book's own slice; mutating it changed the snapshot")
	}

	// Series is a pointer, so its getter must copy the value. SortTitle is here
	// because nothing else reaches it.
	_ = held.SortTitle()
	s := held.Series()
	if s == nil {
		t.Fatal("Series() = nil, want the series set before the search")
	}
	s.Name = "Injected"
	if held.Series().Name != "Saga" {
		t.Error("Series() handed out the book's own SeriesRef; mutating it changed the snapshot")
	}
}

func TestClosedEpubReaderIsErrClosed(t *testing.T) {
	lib := openTestLibrary(t)
	b := ingestTestEpub(t, lib, buildTestEpub(t, "Dune", "Frank Herbert"))

	r, err := lib.Content(b.ID())
	if err != nil {
		t.Fatalf("Content: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// internal/epub's reader_ext_test.go runs this table against
	// epub.ErrClosed. Running it here too shows the facade keeps that
	// behaviour.
	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"ReadAt", func() error { _, err := r.ReadAt(make([]byte, 4), 0); return err }},
		{"OPF", func() error { _, err := r.OPF(); return err }},
		{"Cover", func() error { _, err := r.Cover(); return err }},
		{"Close again", r.Close},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(); !errors.Is(err, library.ErrClosed) {
				t.Errorf("err = %v, want ErrClosed", err)
			}
		})
	}
}

func TestWriteSidecarOverwrites(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Test Book", "Author")
	ingested := ingestTestEpub(t, lib, data)

	// Write initial content
	if err := lib.WriteSidecar(ingested.ID(), "data.txt", []byte("original")); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	// Overwrite it
	if err := lib.WriteSidecar(ingested.ID(), "data.txt", []byte("updated")); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	// Should see the updated content
	got, err := lib.ReadSidecar(ingested.ID(), "data.txt")
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if string(got) != "updated" {
		t.Errorf("ReadSidecar = %q, want %q", got, "updated")
	}
}
