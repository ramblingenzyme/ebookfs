package book

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func content(lib ContentReader, book func() *library.Book) (library.EpubReader, error) {
	// nil once the book is removed, though a fid opened before stays live.
	b := book()
	if b == nil {
		return nil, errors.New("book snapshot not available")
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
