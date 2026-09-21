package book

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// snapshot returns the book the file serves. present says whether the backend
// that reads it was wired up; a BookDir built without one, as the benchmark
// does, reports missing. Both failures are worded here because every file in
// this package meets them, the second when a book is removed after its fid was
// opened.
// ponytail: remove present arg, likely after deletinng benchmark tests
func snapshot(present bool, missing string, book func() *library.Book) (*library.Book, error) {
	if !present {
		return nil, errors.New(missing)
	}
	b := book()
	if b == nil {
		return nil, errors.New("book snapshot not available")
	}
	return b, nil
}

func content(lib ContentReader, book func() *library.Book) (library.EpubReader, error) {
	b, err := snapshot(lib != nil, "library not available", book)
	if err != nil {
		return nil, err
	}
	return lib.Content(b.ID())
}

// contentBytes is a vfile.SnapshotFile loader returning what `pick` takes from the open epub.
func contentBytes(lib ContentReader, book func() *library.Book, pick func(library.EpubReader) ([]byte, error)) func() ([]byte, error) {
	return func() ([]byte, error) {
		r, err := content(lib, book)
		if err != nil {
			return nil, err
		}
		defer r.Close()
		return pick(r)
	}
}
