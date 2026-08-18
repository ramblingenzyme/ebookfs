package library

import (
	"errors"
	"testing"
)

// The duplicate rule is "same title, same set of authors" — a set, so the same
// book credited in either order is one book. It used to be a path lookup, which
// made "A & B" and "B & A" two different directories and two library entries.
func TestIngestDuplicateIgnoresAuthorOrder(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Good Omens", "Neil Gaiman", "Terry Pratchett"))

	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(buildTestEpub(t, "Good Omens", "Terry Pratchett", "Neil Gaiman"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := h.Ingest(); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("second ingest err = %v, want ErrDuplicate", err)
	}
}

// A different author set with the same title is a different book, so it must
// still ingest — the narrowing query matches it, only the set comparison rejects.
func TestIngestSameTitleDifferentAuthors(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Bob"))
}

// A book the indexer skipped is absent from the books table, so Index.Exists
// cannot see it. Ingesting it again would leave two copies on disk with only
// one of them indexed; the store guard is what refuses.
func TestIngestRejectsUnindexedBookOnDisk(t *testing.T) {
	lib := openTestLibrary(t).(*libraryImpl)
	data := buildTestEpub(t, "Orphaned", "Alice")
	b := ingestTestEpub(t, lib, data)

	// Drop the book from the index only, leaving its files in the tree — the
	// state a skipped directory is in after a rebuild.
	op := lib.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := op.Delete(b.Meta.ID); err != nil {
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
