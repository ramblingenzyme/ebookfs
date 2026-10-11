package library_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestSidecarsRoundTrip(t *testing.T) {
	lib := openTestLibrary(t)
	id := ingestTestEpub(t, lib, buildTestEpub(t, "Test Book", "Author")).ID()

	read := func(name, want string) {
		t.Helper()
		got, err := lib.ReadSidecar(id, name)
		if err != nil {
			t.Fatalf("ReadSidecar(%q): %v", name, err)
		}
		if string(got) != want {
			t.Errorf("ReadSidecar(%q) = %q, want %q", name, got, want)
		}
	}

	for _, data := range []string{"original", "updated"} {
		if err := lib.WriteSidecar(id, "notes.txt", []byte(data)); err != nil {
			t.Fatalf("WriteSidecar: %v", err)
		}
		read("notes.txt", data)
	}

	if err := lib.WithSidecars(id, func(r *os.Root) error {
		return r.WriteFile("custom.txt", []byte("custom content"), 0644)
	}); err != nil {
		t.Fatalf("WithSidecars: %v", err)
	}
	read("custom.txt", "custom content")

	if _, err := lib.ReadSidecar(id, "nonexistent.txt"); err == nil {
		t.Error("ReadSidecar should fail for a sidecar never written")
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
