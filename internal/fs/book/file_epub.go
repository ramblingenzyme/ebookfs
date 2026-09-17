package book

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
)

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Size and name are read from the
// book snapshot (set during parse), so Stat never touches the disk.
type epubFile struct {
	vfile.ReadAtFile
	book func() *library.Book
}

func newEpubFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *epubFile {
	return &epubFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (library.EpubReader, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			b := book()
			if b == nil {
				return nil, errors.New("book snapshot not available")
			}
			r, err := lib.Content(b.ID())
			if err != nil {
				return nil, err
			}
			return r, nil
		}),
		book: book,
	}
}

func (e *epubFile) Stat() proto.Stat {
	s := e.BaseFile.Stat()
	if b := e.book(); b != nil {
		s.Name = b.Filename()
		s.Length = uint64(b.EpubSize())
	}
	return s
}
