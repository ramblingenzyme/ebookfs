package fs

import (
	"errors"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
type opfFile struct {
	snapshotFile
}

func newOPFFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *opfFile {
	return &opfFile{
		snapshotFile: newSnapshotFile(stat, func() ([]byte, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			return lib.ExtractOPF(book())
		}),
	}
}

func (o *opfFile) Stat() proto.Stat {
	s := o.BaseFile.Stat()
	if data, err := o.load(); err == nil {
		s.Length = uint64(len(data))
	}
	return s
}

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	snapshotFile
	lib    library.Library
	book   func() *model.Book
	writes map[uint64][]byte
}

func newCoverFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *coverFile {
	return &coverFile{
		snapshotFile: newSnapshotFile(stat, func() ([]byte, error) {
			if lib == nil {
				return nil, errors.New("library not available")
			}
			return lib.ExtractCover(book())
		}),
		lib:    lib,
		book:   book,
		writes: make(map[uint64][]byte),
	}
}

func (c *coverFile) Stat() proto.Stat {
	s := c.BaseFile.Stat()
	if data, err := c.load(); err == nil {
		s.Length = uint64(len(data))
	}
	return s
}

func (c *coverFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
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
	return c.lib.WriteCover(c.book().Meta.ID, data)
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Stat is live: it reports the
// current EpubFilename and on-disk size on each call.
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
	b := e.book()
	s := e.BaseFile.Stat()
	s.Name = b.EpubFilename
	// TODO: b.Stat() calls os.Stat on the epub path. During a rename
	// (title/authors edit) the file is in flight between store.Move and the
	// registry's snapshot swap; this Stat could get a stale path or fail.
	if fi, err := b.Stat(); err == nil {
		s.Length = uint64(fi.Size())
	}
	return s
}
