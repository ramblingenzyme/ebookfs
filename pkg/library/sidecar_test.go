package library_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestReadSidecar(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Test Book", "Author")
	ingested := ingestTestEpub(t, lib, data)

	// Write a sidecar file
	sidecarData := []byte("test sidecar content")
	if err := lib.WriteSidecar(ingested.ID(), "notes.txt", sidecarData); err != nil {
		t.Fatalf("WriteSidecar: %v", err)
	}

	// Read it back
	got, err := lib.ReadSidecar(ingested.ID(), "notes.txt")
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if string(got) != string(sidecarData) {
		t.Errorf("ReadSidecar = %q, want %q", got, sidecarData)
	}
}

func TestWithSidecars(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Test Book", "Author")
	ingested := ingestTestEpub(t, lib, data)

	// Use WithSidecars to write a file
	err := lib.WithSidecars(ingested.ID(), func(r *os.Root) error {
		return r.WriteFile("custom.txt", []byte("custom content"), 0644)
	})
	if err != nil {
		t.Fatalf("WithSidecars: %v", err)
	}

	// Read it back with ReadSidecar
	got, err := lib.ReadSidecar(ingested.ID(), "custom.txt")
	if err != nil {
		t.Fatalf("ReadSidecar: %v", err)
	}
	if string(got) != "custom content" {
		t.Errorf("ReadSidecar = %q, want %q", got, "custom content")
	}
}

func TestSidecarNotFound(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Test Book", "Author")
	ingested := ingestTestEpub(t, lib, data)

	// Try to read a non-existent sidecar
	_, err := lib.ReadSidecar(ingested.ID(), "nonexistent.txt")
	if err == nil {
		t.Error("ReadSidecar should fail for non-existent file")
	}
}

// The index still lists a book removed outside ebookfs until the next walk.
func TestSidecarAccessDoesNotRecreateARemovedBookDirectory(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	ingested := ingestTestEpub(t, lib, buildTestEpub(t, "Test Book", "Author"))
	dir := filepath.Join(cfg.Root, ingested.Dir())
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := lib.ReadSidecar(ingested.ID(), "notes.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("ReadSidecar err = %v, want fs.ErrNotExist", err)
	}
	if _, err := os.Stat(dir); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("book directory exists again after sidecar access (stat err = %v)", err)
	}
}

func TestSidecarBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	// Try to access sidecars for a non-existent book
	err := lib.WithSidecars(999, func(r *os.Root) error {
		return nil
	})
	if err == nil {
		t.Error("WithSidecars should fail for non-existent book")
	}
}
