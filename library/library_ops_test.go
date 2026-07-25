package library

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestLibraryImplOpenEpub(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Open Read"))
	id := book.Meta.ID

	r, err := lib.OpenEpub(id)
	if err != nil {
		t.Fatalf("OpenEpub: %v", err)
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

func TestLibraryImplOpenEpubNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	_, err := lib.OpenEpub(99999)
	if err == nil {
		t.Fatal("expected error for non-existent book")
	}
}

func TestLibraryImplExtractCover(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Cover Test"))
	id := book.Meta.ID

	data, err := lib.ExtractCover(id)
	if err != nil {
		t.Fatalf("ExtractCover: %v", err)
	}
	if string(data) != "placeholder-cover-bytes" {
		t.Errorf("cover data = %q, want %q", string(data), "placeholder-cover-bytes")
	}
}

func TestLibraryImplExtractOPF(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "OPF Title"))
	id := book.Meta.ID

	data, err := lib.ExtractOPF(id)
	if err != nil {
		t.Fatalf("ExtractOPF: %v", err)
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
	if !e.Includes(book) {
		t.Errorf("Includes should return true for a book with status %q", book.Meta.Status)
	}
	book.Meta.Status = "read"
	if e.Includes(book) {
		t.Errorf("Includes should return false for a book with status %q", book.Meta.Status)
	}
}

func TestLibraryImplExporterConvertRequiresCacheDir(t *testing.T) {
	lib := openTestLibrary(t)

	// convert=true without a cache_dir — newExporter accepts it (the validation
	// is in config.Load, not here), but the Exporter should still succeed.
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
	if got[0].Meta.ID != want.Meta.ID {
		t.Errorf("id = %d, want %d", got[0].Meta.ID, want.Meta.ID)
	}
	if got[0].EpubPath == "" {
		t.Fatal("EpubPath is empty — the result carries only the index's relative path, so nothing can open it")
	}
	if !filepath.IsAbs(got[0].EpubPath) {
		t.Errorf("EpubPath = %q, want an absolute path", got[0].EpubPath)
	}
	if _, err := os.Stat(got[0].EpubPath); err != nil {
		t.Errorf("EpubPath %q does not resolve: %v", got[0].EpubPath, err)
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
