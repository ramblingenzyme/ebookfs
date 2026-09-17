package book

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
)

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	vfile.SnapshotFile
	book func() *library.Book
}

func newOPFFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *opfFile {
	return &opfFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, func() ([]byte, error) {
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
			defer r.Close()
			return r.OPF()
		}),
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
