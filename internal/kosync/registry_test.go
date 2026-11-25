package kosync

import (
	"os"
	"testing"
)

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

// The corrupt file stays as it was, since the progress in it may still be
// recoverable by hand.
func TestRestoreSkipsCorruptSidecars(t *testing.T) {
	lib := openTestLibrary(t)

	book1 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Good Book", "Alice"))
	book2 := ingestTestEpub(t, lib.Library, buildTestEpub(t, "Corrupt Book", "Bob"))

	const corrupt = "{corrupt json"

	if err := lib.Library.WithSidecars(book2.ID(), func(root *os.Root) error {
		return root.WriteFile("kosync.json", []byte(corrupt), 0o644)
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

	raw, err := lib.Library.ReadSidecar(book2.ID(), "kosync.json")
	if err != nil {
		t.Fatalf("reading corrupt sidecar after Restore: %v", err)
	}

	if string(raw) != corrupt {
		t.Errorf("corrupt sidecar = %q after Restore, want it left as %q", raw, corrupt)
	}
}
