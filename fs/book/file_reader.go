package book

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ReaderFile serves a book's export rendition through the Exporter, holding one
// reader per fid. It mirrors epubFile, but its size is reported live from the
// exporter so a kepub's length appears once its cache is warm. It is exported
// because the reader view (fs/views) constructs it directly.
type ReaderFile struct {
	vfile.ReadAtFile
	exp  library.Exporter
	book func() *model.Book
}

func NewReaderFile(stat *proto.Stat, exp library.Exporter, book func() *model.Book) *ReaderFile {
	return &ReaderFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (model.EpubReader, error) {
			if exp == nil {
				return nil, errors.New("exporter not available")
			}
			b := book()
			if b == nil {
				return nil, errors.New("book snapshot not available")
			}
			r, err := exp.Open(b)
			if err != nil {
				return nil, err
			}
			return r, nil
		}),
		exp:  exp,
		book: book,
	}
}

// Stat reports the export size when known (cheap; never triggers a conversion),
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
