package book

import (
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path.
type epubFile struct {
	vfile.ReadAtFile
	book func() *library.Book
}

func newEpubFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *epubFile {
	return &epubFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (vfile.ReaderAtCloser, error) {
			return content(lib, book)
		}),
		book: book,
	}
}

// Stat reports the epub's current filename as well as its length, so a retitled
// book's entry is renamed without rebuilding the node.
func (e *epubFile) Stat() proto.Stat {
	s := e.BaseFile.Stat()
	if b := e.book(); b != nil {
		s.Name = b.Filename()
		s.Length = uint64(b.EpubSize())
	}
	return s
}
