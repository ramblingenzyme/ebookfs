package book

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ingestedBook opens a real library holding one book.
func ingestedBook(t *testing.T) (*library.Library, *library.Book) {
	t.Helper()
	lib, err := library.Open(library.Config(util.TestConfig(t)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lib.Close() })
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.WriteAt(util.BuildTestEpub(t, "Dune", "Frank Herbert"), 0); err != nil {
		t.Fatal(err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatal(err)
	}
	return lib, b
}
