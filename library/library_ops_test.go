package library

import (
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

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

func TestLibraryImplExporter(t *testing.T) {
	lib := openTestLibrary(t)

	e, err := lib.Exporter(config.ReaderConfig{
		Statuses: []string{"unread"},
	})
	if err != nil {
		t.Fatalf("Exporter: %v", err)
	}
	if e == nil {
		t.Fatal("Exporter returned nil")
	}
	book := makeBook(1, "Test", "Author")
	book.Meta.Status = "unread"
	if !e.Includes(testutil.WrapBook(book)) {
		t.Errorf("Includes should return true for a book with status %q", book.Meta.Status)
	}
	book.Meta.Status = "read"
	if e.Includes(testutil.WrapBook(book)) {
		t.Errorf("Includes should return false for a book with status %q", book.Meta.Status)
	}
}

func TestLibraryImplExporterConvertRequiresCacheDir(t *testing.T) {
	lib := openTestLibrary(t)

	// Exporter succeeds with a cache_dir supplied alongside convert=true.
	e, err := lib.Exporter(config.ReaderConfig{
		Statuses: []string{"unread", "reading"},
		Convert:  true,
		CacheDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("Exporter with convert: %v", err)
	}
	if e == nil {
		t.Fatal("Exporter returned nil")
	}
}

// TestLibraryImplSearchHydratesEpubPath covers the half of Search the index
// cannot do. The index stores a library-relative path; every consumer of a
// result — the exporter, the 9P epub file — needs an absolute one, so Search
// fills it in on the way out. Query has the same loop and its own coverage;
// Search's copy had none, so deleting it broke no test while leaving every
// search result unopenable.
func TestLibraryImplSearchHydratesEpubPath(t *testing.T) {
	lib := openTestLibrary(t)
	want := ingestTestEpub(t, lib, buildTestEpub(t, "Findable", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Unrelated", "Bob"))

	got, err := lib.Search(model.Query{Titles: []string{"Findable"}})
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
	absPath := lib.(*libraryImpl).store.AbsPath(got[0].EpubPath())
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

	got, err := lib.Search(model.Query{Titles: []string{"nothing matches this"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Search returned %d books, want none", len(got))
	}
}
