package library

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEditSelfHealsLegacyUnsanitizedFilename reproduces a production bug: a
// book ingested before FAT sanitization was consistently applied has an
// on-disk epub filename containing FAT-illegal characters (e.g. ':'), while
// its LibraryPath (built from the raw, unsanitized title) still matches what
// Layout() recomputes. An Edit() that only touches an unrelated field
// (Status) leaves Title/Authors unchanged, so Layout() recomputes the same
// LibraryPath but a different (sanitized) EpubFilename, and Move() must
// handle this same-directory rename rather than fail on "destination already
// exists".
func TestEditSelfHealsLegacyUnsanitizedFilename(t *testing.T) {
	lib := openTestLibrary(t)
	title := "Some Title: With Colon"
	book := ingestTestEpub(t, lib, buildTestEpub(t, title))
	id := book.ID()

	// Simulate legacy drift: rename the on-disk epub to an unsanitized name
	// that Layout() would no longer produce, then reindex so the index's
	// stored EpubFilename reflects the manual rename.
	root := lib.(*libraryImpl).store.Root()
	dir := filepath.Join(root, filepath.Dir(book.EpubPath()))
	absEpub := filepath.Join(root, book.EpubPath())
	legacyName := "Some Title: With Colon - Alice.epub"
	if err := os.Rename(absEpub, filepath.Join(dir, legacyName)); err != nil {
		t.Fatal(err)
	}
	if err := lib.Reindex(); err != nil {
		t.Fatalf("Reindex: %v", err)
	}

	// Edit an unrelated field (Status) — Title/Authors untouched. This used
	// to hit "destination already exists" and fail every time.
	status := "reading"
	updated, err := lib.Edit(id, Edits{Status: &status})
	if err != nil {
		t.Fatalf("Edit: %v (same-directory Move must not fail)", err)
	}
	if updated.Status() != status {
		t.Errorf("status = %q, want %q", updated.Status(), status)
	}

	// Self-heal: filename should now be FAT-sanitized.
	if strings.Contains(filepath.Base(updated.EpubPath()), ":") {
		t.Errorf("filename not sanitized after edit: %q", filepath.Base(updated.EpubPath()))
	}
	if _, err := os.Stat(filepath.Join(root, updated.EpubPath())); err != nil {
		t.Errorf("updated EpubPath does not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, legacyName)); !os.IsNotExist(err) {
		t.Errorf("legacy filename still present after self-heal")
	}

	// A second edit must also succeed (regression: the original bug caused
	// every subsequent edit to fail identically, not just the first).
	status2 := "read"
	if _, err := lib.Edit(id, Edits{Status: &status2}); err != nil {
		t.Fatalf("second Edit: %v", err)
	}
}
