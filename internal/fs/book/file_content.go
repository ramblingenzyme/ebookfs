package book

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
	"github.com/ramblingenzyme/go9p/proto"
)

// contentFile serves a book's file through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path.
type contentFile struct {
	vfile.ReadAtFile
	book func() *library.Book
}

func newContentFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *contentFile {
	return &contentFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (vfile.ReaderAtCloser, error) {
			return content(lib, book)
		}),
		book: book,
	}
}

// Stat reports the book's current filename as well as its length, so a retitled
// book's entry is renamed without rebuilding the node.
func (e *contentFile) Stat() proto.Stat {
	s := e.BaseFile.Stat()
	if b := e.book(); b != nil {
		s.Name = b.Filename()
		s.Length = uint64(b.Size())
	}
	return s
}
