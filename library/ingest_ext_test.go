package library_test

import (
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"
)

// The duplicate rule is "same title, same set of authors", a set, so the same
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
// still ingest: the narrowing query matches it, only the set comparison rejects.
func TestIngestSameTitleDifferentAuthors(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Bob"))
}

// A file that is not an epub fails the ingest with an error a caller can name.
// The sentinel is the epub package's, surfaced here because this is where a
// caller meets it: the 9P inbox reports an upload it could not file, and
// "not an epub" is the one case the person who uploaded it can fix.
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
