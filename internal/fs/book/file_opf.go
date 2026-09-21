package book

import (
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	vfile.SnapshotFile
	book func() *library.Book
}

func newOPFFile(stat *proto.Stat, lib ContentReader, book func() *library.Book) *opfFile {
	return &opfFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, contentBytes(lib, book, library.EpubReader.OPF)),
		book:         book,
	}
}

func (o *opfFile) Stat() proto.Stat {
	s := o.BaseFile.Stat()
	if b := o.book(); b != nil {
		s.Length = uint64(b.OpfSize())
	}
	return s
}
