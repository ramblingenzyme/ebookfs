package book

import (
	"errors"

	"github.com/ramblingenzyme/go9p/proto"

	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	vfile.SnapshotFile
	book func() *library.Book
}

// opfReader is the epub-specific interface satisfied by the concrete reader
// returned from Library.Content for epub books.
type opfReader interface {
	OPF() ([]byte, error)
}

func newOPFFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *opfFile {
	return &opfFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, contentBytes(lib, book, func(r library.Reader) ([]byte, error) {
			e, ok := r.(opfReader)
			if !ok {
				return nil, errors.New("format does not support OPF")
			}

			return e.OPF()
		})),
		book: book,
	}
}

func (o *opfFile) Stat() proto.Stat {
	s := o.BaseFile.Stat()
	if b := o.book(); b != nil {
		s.Length = uint64(b.OpfSize())
	}

	return s
}
