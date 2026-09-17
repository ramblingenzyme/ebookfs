package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsReindexClosedIndex(t *testing.T) {
	lib := openTestLibrary(t)
	lib.index.Close()

	// With a closed index, NeedsReindex returns an error, so needsReindex
	// must return true to force a rebuild on the next Open.
	if !lib.needsReindex() {
		t.Error("needsReindex should return true when index check fails")
	}
}

func TestCheckSameFilesystemMissingTarget(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent")

	err := checkSameFilesystem(dir, missing)
	if err == nil {
		t.Fatal("expected error when target directory doesn't exist")
	}
}

func TestCreateIngestReadOnlyDir(t *testing.T) {
	lib := openTestLibrary(t)

	if err := os.Chmod(lib.inboxTemp, 0444); err != nil {
		t.Skip("cannot chmod inbox temp:", err)
	}
	t.Cleanup(func() { os.Chmod(lib.inboxTemp, 0755) })

	_, err := lib.CreateIngest()
	if err == nil {
		t.Error("expected error when inbox temp is read-only")
	}
}

// Every id-addressed read reports a missing book the same way, so a caller can
// test one sentinel whichever it called.
func TestGetMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	if _, err := lib.Content(999); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("Content(999) err = %v, want ErrBookNotFound", err)
	}
	if _, err := lib.Get(999); !errors.Is(err, ErrBookNotFound) {
		t.Errorf("Get(999) err = %v, want ErrBookNotFound", err)
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
