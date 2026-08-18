package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestNeedsReindexClosedIndex(t *testing.T) {
	lib := openTestLibrary(t).(*libraryImpl)
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
	lib := openTestLibrary(t).(*libraryImpl)

	if err := os.Chmod(lib.inboxTemp, 0444); err != nil {
		t.Skip("cannot chmod inbox temp:", err)
	}
	t.Cleanup(func() { os.Chmod(lib.inboxTemp, 0755) })

	_, err := lib.CreateIngest()
	if err == nil {
		t.Error("expected error when inbox temp is read-only")
	}
}

func TestGetMissingBookIsErrBookNotFound(t *testing.T) {
	lib := openTestLibrary(t)

	_, err := lib.Content(999)
	if !errors.Is(err, ErrBookNotFound) {
		t.Fatalf("Content(999) err = %v, want ErrBookNotFound", err)
	}
}
