package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestReadMetaRoundTrip(t *testing.T) {
	s, root := newStore(t)

	writeBook(t, root, "Author, A/Test (1)", "", "", nil)

	loc := book.Location{EpubPath: filepath.Join("Author, A/Test (1)", "test.epub")}
	original := &book.Meta{
		ID:     42,
		Status: "reading",
		Rating: 3.5,
		Tags:   []string{"sci-fi", "classic"},
	}

	if err := s.writeMeta(loc, original); err != nil {
		t.Fatalf("writeMeta: %v", err)
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
	err := writeMeta(path, &book.Meta{ID: 1})
	if err == nil {
		t.Error("expected error writing meta.toml to read-only directory")
	}
}
