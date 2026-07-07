package fs

import (
	"errors"
	"fmt"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const maxCoverFileSize = 32 << 20 // 32 MiB

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	snapshotFile
	book func() *model.Book
}

func newOPFFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *opfFile {
	return &opfFile{
		snapshotFile: newSnapshotFile(stat, func() ([]byte, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			return lib.ExtractOPF(book())
		}),
		book: book,
	}
}

func (o *opfFile) Stat() proto.Stat {
	s := o.BaseFile.Stat()
	if b := o.book(); b != nil {
		s.Length = uint64(b.OpfSize)
	}
	return s
}

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	snapshotFile
	edit   func(int64, model.Edits) error
	book   func() *model.Book
	writes map[uint64][]byte
}

func newCoverFile(stat *proto.Stat, lib library.Library, edit func(int64, model.Edits) error, book func() *model.Book) *coverFile {
	return &coverFile{
		snapshotFile: newSnapshotFile(stat, func() ([]byte, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			return lib.ExtractCover(book())
		}),
		edit:   edit,
		book:   book,
		writes: make(map[uint64][]byte),
	}
}

func (c *coverFile) Stat() proto.Stat {
	s := c.BaseFile.Stat()
	if b := c.book(); b != nil {
		s.Length = uint64(b.CoverSize)
	}
	return s
}

func (c *coverFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	if offset+uint64(len(data)) > maxCoverFileSize {
		return 0, fmt.Errorf("write exceeds cover file size limit (%d bytes)", maxCoverFileSize)
	}
	c.Lock()
	defer c.Unlock()
	end := offset + uint64(len(data))
	buf := c.writes[fid]
	if end > uint64(len(buf)) {
		buf = append(buf, make([]byte, end-uint64(len(buf)))...)
	}
	copy(buf[offset:], data)
	c.writes[fid] = buf
	return uint32(len(data)), nil
}

func (c *coverFile) Close(fid uint64) error {
	c.Lock()
	defer c.Unlock()
	data := c.writes[fid]
	delete(c.reads, fid)
	delete(c.writes, fid)
	if len(data) == 0 {
		return nil
	}
	return c.edit(c.book().Meta.ID, model.Edits{Cover: &data})
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Size and name are read from the
// book snapshot (set during parse), so Stat never touches the disk.
type epubFile struct {
	readAtFile
	book func() *model.Book
}

func newEpubFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *epubFile {
	return &epubFile{
		readAtFile: newReadAtFile(stat, func() (library.EpubReader, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			return lib.OpenEpub(book())
		}),
		book: book,
	}
}

func (e *epubFile) Stat() proto.Stat {
	s := e.BaseFile.Stat()
	if b := e.book(); b != nil {
		s.Name = b.EpubFilename
		s.Length = uint64(b.EpubSize)
	}
	return s
}
