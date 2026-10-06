package library_test

import (
	"os"
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

func TestWriteSidecarAtomic(t *testing.T) {
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
