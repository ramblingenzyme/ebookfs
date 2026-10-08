package kosync

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRebuildFromMissingMapping verifies that Rebuild() restores the mapping
// when the mapping file is deleted.
func TestRebuildFromMissingMapping(t *testing.T) {
	lib, mapping := setupMapping(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Book One", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Book Two", "Bob"))

	if len(mapping.Snapshot()) != 2 {
		t.Fatalf("mapping has %d entries, want 2", len(mapping.Snapshot()))
	}

	if err := os.Remove(mapping.path); err != nil {
		t.Fatalf("removing mapping: %v", err)
	}

	newMapping := NewEmptyMapping(filepath.Dir(mapping.path))
	if err := newMapping.Rebuild(lib.Library); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	newSnapshot := newMapping.Snapshot()
	if len(newSnapshot) != 2 {
		t.Errorf("rebuilt mapping has %d entries, want 2", len(newSnapshot))
	}

	for docID, bookID := range newSnapshot {
		if bookID != book1.ID() && bookID != book2.ID() {
			t.Errorf("unexpected book_id %d for document %s", bookID, docID)
		}
	}
}

// TestRebuildFromCorruptMapping verifies that Rebuild() overwrites a corrupt
// mapping file with correct data from sidecars.
func TestRebuildFromCorruptMapping(t *testing.T) {
	lib, mapping := setupMapping(t)

	book := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Corrupt Test", "Alice"))

	if len(mapping.Snapshot()) != 1 {
		t.Fatalf("mapping has %d entries, want 1", len(mapping.Snapshot()))
	}

	if err := os.WriteFile(mapping.path, []byte("{invalid json"), 0644); err != nil {
		t.Fatalf("corrupting mapping: %v", err)
	}

	if _, err := LoadMapping(filepath.Dir(mapping.path)); err == nil {
		t.Fatal("LoadMapping succeeded on corrupt file, want error")
	}

	newMapping := NewEmptyMapping(filepath.Dir(mapping.path))
	if err := newMapping.Rebuild(lib.Library); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	newSnapshot := newMapping.Snapshot()
	if len(newSnapshot) != 1 {
		t.Errorf("rebuilt mapping has %d entries, want 1", len(newSnapshot))
	}

	for _, bookID := range newSnapshot {
		if bookID != book.ID() {
			t.Errorf("book_id = %d, want %d", bookID, book.ID())
		}
	}
}

// TestRebuildSkipsBooksWithoutSidecars verifies that Rebuild() gracefully
// handles books that don't have kosync sidecars.
func TestRebuildSkipsBooksWithoutSidecars(t *testing.T) {
	lib, mapping := setupMapping(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "With Sidecar", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Without Sidecar", "Bob"))

	if len(mapping.Snapshot()) != 2 {
		t.Fatalf("mapping has %d entries, want 2", len(mapping.Snapshot()))
	}

	if err := lib.Library.WithSidecars(book2.ID(), func(root *os.Root) error {
		return root.Remove("kosync.json")
	}); err != nil {
		t.Fatalf("removing sidecar: %v", err)
	}

	if err := os.Remove(mapping.path); err != nil {
		t.Fatalf("removing mapping: %v", err)
	}

	newMapping := NewEmptyMapping(filepath.Dir(mapping.path))
	if err := newMapping.Rebuild(lib.Library); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	newSnapshot := newMapping.Snapshot()
	if len(newSnapshot) != 1 {
		t.Errorf("rebuilt mapping has %d entries, want 1", len(newSnapshot))
	}

	for _, bookID := range newSnapshot {
		if bookID != book1.ID() {
			t.Errorf("book_id = %d, want %d (book2 should have no sidecar)", bookID, book1.ID())
		}
	}
}

// TestRebuildSkipsCorruptSidecars verifies that Rebuild() skips books with
// corrupt sidecars and continues rebuilding the rest.
func TestRebuildSkipsCorruptSidecars(t *testing.T) {
	lib, mapping := setupMapping(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Good Book", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Corrupt Book", "Bob"))

	if len(mapping.Snapshot()) != 2 {
		t.Fatalf("mapping has %d entries, want 2", len(mapping.Snapshot()))
	}

	if err := lib.Library.WithSidecars(book2.ID(), func(root *os.Root) error {
		return root.WriteFile("kosync.json", []byte("{corrupt json"), 0644)
	}); err != nil {
		t.Fatalf("corrupting sidecar: %v", err)
	}

	if err := os.Remove(mapping.path); err != nil {
		t.Fatalf("removing mapping: %v", err)
	}

	newMapping := NewEmptyMapping(filepath.Dir(mapping.path))
	if err := newMapping.Rebuild(lib.Library); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	newSnapshot := newMapping.Snapshot()
	if len(newSnapshot) != 1 {
		t.Errorf("rebuilt mapping has %d entries, want 1 (corrupt sidecar should be skipped)", len(newSnapshot))
	}

	for _, bookID := range newSnapshot {
		if bookID != book1.ID() {
			t.Errorf("book_id = %d, want %d", bookID, book1.ID())
		}
	}
}

// TestRebuildEmptyLibrary verifies that Rebuild() works correctly on an
// empty library.
func TestRebuildEmptyLibrary(t *testing.T) {
	lib := openTestLibrary(t)
	mapping := NewEmptyMapping(t.TempDir())

	if err := mapping.Rebuild(lib.Library); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	if len(mapping.Snapshot()) != 0 {
		t.Errorf("rebuilt mapping has %d entries, want 0", len(mapping.Snapshot()))
	}
}
