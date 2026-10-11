package book

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func newTestEpubFile(t *testing.T, name string, lib ContentReader, get func() *library.Book) *epubFile {
	t.Helper()
	return newEpubFile(newStat(util.NewTestFS(t), name, 0444), lib, get)
}

// Stat reports the book's current filename and the size the index recorded,
// without touching the disk. Building the file as stale.epub over a path that
// does not exist shows both. With no book, it keeps the name it was built with.
func TestEpubFileStat(t *testing.T) {
	recorded := func(size int64) func() *library.Book {
		book := util.MakeMutableBook(1, "Test", "Author")
		book.EpubPath = "/nonexistent/book.epub"
		book.EpubSize = size
		return util.Fixed(util.WrapBook(book))
	}
	for _, tc := range []struct {
		name     string
		book     func() *library.Book
		wantName string
		wantLen  uint64
	}{
		{"no size recorded", recorded(0), "book.epub", 0},
		{"size recorded", recorded(17), "book.epub", 17},
		{"no book", func() *library.Book { return nil }, "stale.epub", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestEpubFile(t, "stale.epub", mock.ContentReader{}, tc.book).Stat()
			if s.Name != tc.wantName || s.Length != tc.wantLen {
				t.Errorf("Stat = (%q, %d), want (%q, %d)", s.Name, s.Length, tc.wantName, tc.wantLen)
			}
		})
	}
}
