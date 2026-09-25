package library_test

import (
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// The duplicate rule compares author sets, so either order is one book.
// Regression: a path lookup filed "A & B" and "B & A" as two books.
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

// The title query matches it; only the author set tells the two apart.
func TestIngestSameTitleDifferentAuthors(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Bob"))
}

// Not an epub is the one ingest failure the uploader can fix, so it has a
// sentinel.
func TestIngestRejectsAFileThatIsNotAnEpub(t *testing.T) {
	lib := openTestLibrary(t)

	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt([]byte("this is plainly not a zip archive"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := h.Ingest(); !errors.Is(err, library.ErrNotEpub) {
		t.Fatalf("ingest err = %v, want ErrNotEpub", err)
	}
}
