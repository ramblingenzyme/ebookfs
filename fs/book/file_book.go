package book

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// newStat is the package-local shorthand for vfile.NewStat, the single
// definition of the glenda/glenda owner convention every node uses.
var newStat = vfile.NewStat

const maxCoverFileSize = 32 << 20 // 32 MiB

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	vfile.SnapshotFile
	book func() *library.Book
}

func newOPFFile(stat *proto.Stat, lib library.Library, book func() *library.Book) *opfFile {
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

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	vfile.SnapshotFile
	edit   func(int64, model.Edits) error
	book   func() *library.Book
	writes vfile.WriteBuffer
}

func newCoverFile(stat *proto.Stat, lib library.Library, edit func(int64, model.Edits) error, book func() *library.Book) *coverFile {
	return &coverFile{
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
			return r.Cover()
		}),
		edit:   edit,
		book:   book,
		writes: vfile.NewWriteBuffer(maxCoverFileSize),
	}
}

func (c *coverFile) Stat() proto.Stat {
	s := c.BaseFile.Stat()
	if b := c.book(); b != nil {
		s.Length = uint64(b.CoverSize())
	}
	return s
}

func (c *coverFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	// A cover is replaced wholesale, never edited from the current bytes, so the
	// buffer starts empty (nil seed).
	return c.writes.Write(fid, offset, data, nil)
}

func (c *coverFile) Close(fid uint64) error {
	data := c.writes.Take(fid)
	c.SnapshotFile.Close(fid)
	if len(data) == 0 {
		return nil
	}
	return c.edit(c.book().ID(), model.Edits{Cover: &data})
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Size and name are read from the
// book snapshot (set during parse), so Stat never touches the disk.
type epubFile struct {
	vfile.ReadAtFile
	book func() *library.Book
}

func newEpubFile(stat *proto.Stat, lib library.Library, book func() *library.Book) *epubFile {
	return &epubFile{
		ReadAtFile: vfile.NewReadAtFile(stat, func() (model.EpubReader, error) {
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
