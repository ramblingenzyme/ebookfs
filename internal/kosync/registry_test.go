package kosync

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// TestRestoreEmptyLibrary verifies that Restore works on an empty library.
func TestRestoreEmptyLibrary(t *testing.T) {
	lib := openTestLibrary(t)

	r := newDocRegistry()
	if err := r.Restore(lib.Library); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	if snap := r.snapshot(); len(snap) != 0 {
		t.Errorf("restored index has %d entries, want 0", len(snap))
	}
}

// TestRestoreCreatesMissingSidecars verifies Restore computes document IDs and
// writes sidecars for books that arrived without one.
func TestRestoreCreatesMissingSidecars(t *testing.T) {
	lib := openTestLibrary(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Book One", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Book Two", "Bob"))
	books := []*library.Book{book1, book2}

	for _, book := range books {
		if _, err := lib.Library.ReadSidecar(book.ID(), "kosync.json"); err == nil {
			t.Fatalf("book %d has a sidecar before Restore", book.ID())
		}
	}

	r := newDocRegistry()
	if err := r.Restore(lib.Library); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	snap := r.snapshot()
	if len(snap) != 2 {
		t.Fatalf("restored index has %d entries, want 2", len(snap))
	}

	for _, book := range books {
		raw, err := lib.Library.ReadSidecar(book.ID(), "kosync.json")
		if err != nil {
			t.Fatalf("reading sidecar after Restore: %v", err)
		}
		var sidecar SidecarData
		if err := json.Unmarshal(raw, &sidecar); err != nil {
			t.Fatalf("unmarshaling sidecar: %v", err)
		}
		if len(sidecar.DocumentIDs) != 1 || sidecar.DocumentIDs[0] == "" {
			t.Fatalf("book %d sidecar has invalid document IDs: %v", book.ID(), sidecar.DocumentIDs)
		}
		if bookID := snap[sidecar.DocumentIDs[0]]; bookID != book.ID() {
			t.Errorf("index[%s] = %d, want %d", sidecar.DocumentIDs[0], bookID, book.ID())
		}
	}
}

// TestRestoreSkipsCorruptSidecars verifies Restore skips books with corrupt
// sidecars and indexes the rest.
func TestRestoreSkipsCorruptSidecars(t *testing.T) {
	lib := openTestLibrary(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Good Book", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Corrupt Book", "Bob"))

	if err := lib.Library.WithSidecars(book2.ID(), func(root *os.Root) error {
		return root.WriteFile("kosync.json", []byte("{corrupt json"), 0644)
	}); err != nil {
		t.Fatalf("corrupting sidecar: %v", err)
	}

	r := newDocRegistry()
	if err := r.Restore(lib.Library); err != nil {
		t.Fatalf("Restore: %v", err)
	}

	snap := r.snapshot()
	if len(snap) != 1 {
		t.Fatalf("restored index has %d entries, want 1 (corrupt sidecar should be skipped)", len(snap))
	}

	for _, bookID := range snap {
		if bookID != book1.ID() {
			t.Errorf("index book_id = %d, want %d", bookID, book1.ID())
		}
	}
}
