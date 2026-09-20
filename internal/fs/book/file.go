package book

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// content opens the book's epub through lib. Both failures are worded here
// because every file in this package meets them: a BookDir built without a
// library, which the benchmark does, and a book removed since the fid was
// opened.
func content(lib ContentReader, book func() *library.Book) (library.EpubReader, error) {
	if lib == nil {
		return nil, errors.New("library not available")
	}
	b := book()
	if b == nil {
		return nil, errors.New("book snapshot not available")
	}
	return lib.Content(b.ID())
}

// contentBytes is a vfile.SnapshotFile loader returning what pick takes from the
// open epub. The reader is closed before the bytes are handed back, so a caller
// holds no epub beyond the load.
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

// statLen reports base with Length read from the current snapshot. A book
// removed since the fid was opened leaves the length as base carries it.
func statLen(base proto.Stat, book func() *library.Book, size func(*library.Book) int64) proto.Stat {
	if b := book(); b != nil {
		base.Length = uint64(size(b))
	}
	return base
}
