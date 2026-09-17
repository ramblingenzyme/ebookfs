package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestIngest(t *testing.T) {
	s, root := newStore(t)

	tmpEpub := filepath.Join(root, ".inbox-tmp", "staged.epub")
	if err := os.MkdirAll(filepath.Dir(tmpEpub), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tmpEpub, []byte("fake-epub-content"), 0644); err != nil {
		t.Fatal(err)
	}

	loc := s.Layout([]book.Author{{Name: "Alice"}}, "Ingested", 10)
	meta := &book.Meta{ID: 10}

	if _, err := s.Ingest(tmpEpub, loc, meta); err != nil {
		t.Fatalf("Ingest: %v", err)
	}

	bookDir := filepath.Join(root, loc.Dir())
	if _, err := os.Stat(bookDir); err != nil {
		t.Errorf("book directory not created: %v", err)
	}
	gotEpub := filepath.Join(bookDir, loc.Filename())
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

// Ingest returns an error when the sidecar write fails, but does not clean up:
// the caller is responsible for deciding whether to delete the partial
// directory.
func TestIngestSurfacesWriteMetaFailure(t *testing.T) {
	s, root := newStore(t)

	staged := filepath.Join(t.TempDir(), "staged.epub")
	if err := os.WriteFile(staged, []byte("epub"), 0644); err != nil {
		t.Fatal(err)
	}
	loc := book.Location{EpubPath: filepath.Join("Alice/Title (1)", "Title - Alice.epub")}
	// Occupy meta.toml's name with a directory, so the sidecar write fails
	// after the epub is already in place.
	bookDir := filepath.Join(root, loc.Dir())
	if err := os.MkdirAll(filepath.Join(bookDir, metaFilename), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Ingest(staged, loc, &book.Meta{ID: 1}); err == nil {
		t.Fatal("Ingest succeeded with an unwritable meta.toml, want the failure surfaced")
	}
	if _, err := os.Stat(bookDir); os.IsNotExist(err) {
		t.Error("Ingest cleaned up the book directory; caller is responsible for cleanup")
	}
}

// The earlier failure: nothing to move into place. The book directory is
// created before the rename is attempted, so the error is surfaced rather than
// leaving a half-built book behind.
func TestIngestMissingStagedEpub(t *testing.T) {
	s, root := newStore(t)

	loc := book.Location{EpubPath: filepath.Join("Alice/Title (1)", "Title - Alice.epub")}
	_, err := s.Ingest(filepath.Join(t.TempDir(), "does-not-exist.epub"), loc, &book.Meta{ID: 1})
	if err == nil {
		t.Fatal("Ingest succeeded with no staged epub, want the failure surfaced")
	}
	if _, err := os.Stat(filepath.Join(root, loc.Dir(), metaFilename)); !os.IsNotExist(err) {
		t.Error("meta.toml was written despite the epub never arriving")
	}
}
