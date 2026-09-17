package library_test

import (
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"
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
	if _, err := h.Ingest(); !errors.Is(err, library.ErrDuplicate) {
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
