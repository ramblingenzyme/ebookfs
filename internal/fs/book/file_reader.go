package book

import (
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Renderer is what a ReaderFile needs of an exporter: the rendition itself and
// its size. library.Exporter's other four methods belong to the view that files
// the book, not to the file that serves it.
type Renderer interface {
	Open(*library.Book) (library.EpubReader, error)
	Size(*library.Book) (int64, bool) // cheap; false when the rendition is cold
}

// ReaderFile serves a book's export rendition through the Renderer, holding one
// reader per fid. It mirrors epubFile, but its size is reported live from the
// exporter so a kepub's length appears once its cache is warm. It is exported
// because the reader view (fs/views) constructs it directly.
type ReaderFile struct {
	vfile.ReadAtFile
	exp  Renderer
	book func() *library.Book
}

func NewReaderFile(stat *proto.Stat, exp Renderer, book func() *library.Book) *ReaderFile {
	return &ReaderFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (library.EpubReader, error) {
			b, err := snapshot(exp != nil, "exporter not available", book)
			if err != nil {
				return nil, err
			}
			return exp.Open(b)
		}),
		exp:  exp,
		book: book,
	}
}

// Stat reports the export size when known (never triggers a conversion),
// so a cold kepub lists as length 0 until its cache is warm.
func (r *ReaderFile) Stat() proto.Stat {
	s := r.BaseFile.Stat()
	if b := r.book(); b != nil && r.exp != nil {
		if size, ok := r.exp.Size(b); ok {
			s.Length = uint64(size)
		}
	}
	return s
}
