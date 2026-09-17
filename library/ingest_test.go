// The white-box side of ingest's tests; the black-box side is ingest_ext_test.go.
// Reaching the state these pin (a book on disk that the index does not hold, and
// an inbox temp directory that cannot be written) means touching lib.index and
// lib.inboxTemp, which no public call exposes. The assertions themselves are on
// public behaviour: ErrDuplicateOnDisk, and CreateIngest failing rather than
// returning an unusable handle.

package library

import (
	"errors"
	"os"
	"testing"
)

// A book the indexer skipped is absent from the books table, so Index.Exists
// cannot see it. Ingesting it again would leave two copies on disk with only
// one of them indexed; the store guard is what refuses.
func TestIngestRejectsUnindexedBookOnDisk(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Orphaned", "Alice")
	b := ingestTestEpub(t, lib, data)

	// Drop the book from the index only, leaving its files in the tree, the
	// state a skipped directory is in after a rebuild.
	op := lib.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := op.Delete(b.ID()); err != nil {
		t.Fatalf("index delete: %v", err)
	}

	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := h.Ingest(); !errors.Is(err, ErrDuplicateOnDisk) {
		t.Fatalf("re-ingest err = %v, want ErrDuplicateOnDisk", err)
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
