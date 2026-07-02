package fs

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	fs.BaseFile
	lib    library.Library
	book   *model.Book
	reads  map[uint64][]byte
	writes map[uint64][]byte
}

func newCoverFile(stat *proto.Stat, lib library.Library, book *model.Book) *coverFile {
	return &coverFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		reads:    make(map[uint64][]byte),
		writes:   make(map[uint64][]byte),
	}
}

func (c *coverFile) Stat() proto.Stat {
	s := c.BaseFile.Stat()
	if c.lib != nil {
		if data, err := c.lib.ExtractCover(c.book); err == nil {
			s.Length = uint64(len(data))
		}
	}
	return s
}

func (c *coverFile) Open(fid uint64, omode proto.Mode) error {
	data, err := c.lib.ExtractCover(c.book)
	if err != nil {
		return err
	}
	c.Lock()
	c.reads[fid] = data
	c.writes[fid] = nil
	c.Unlock()
	return nil
}

func (c *coverFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	c.RLock()
	defer c.RUnlock()
	data := c.reads[fid]
	if data == nil {
		return nil, errors.New("not open")
	}
	if offset >= uint64(len(data)) {
		return []byte{}, nil
	}
	if offset+count > uint64(len(data)) {
		count = uint64(len(data)) - offset
	}
	return data[offset : offset+count], nil
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
	return c.lib.WriteCover(c.book, data)
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Stat is live: it reports the
// current EpubFilename and on-disk size on each call.
type epubFile struct {
	fs.BaseFile
	lib  library.Library
	book *model.Book
	fids map[uint64]library.EpubReader
}

func newEpubFile(stat *proto.Stat, lib library.Library, book *model.Book) *epubFile {
	return &epubFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		fids:     make(map[uint64]library.EpubReader),
	}
}

func (e *epubFile) Stat() proto.Stat {
	s := e.BaseFile.Stat()
	s.Name = e.book.EpubFilename
	// TODO: e.book.Stat() calls os.Stat on the epub path. During a rename
	// (title/authors edit) the file is in flight between view remove and add;
	// this Stat could get a stale path or fail.
	if fi, err := e.book.Stat(); err == nil {
		s.Length = uint64(fi.Size())
	}
	return s
}

func (e *epubFile) Open(fid uint64, omode proto.Mode) error {
	r, err := e.lib.OpenEpub(e.book)
	if err != nil {
		return err
	}
	e.Lock()
	e.fids[fid] = r
	e.Unlock()
	return nil
}

func (e *epubFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	e.RLock()
	defer e.RUnlock()
	r := e.fids[fid]
	if r == nil {
		return nil, errors.New("not open")
	}
	buf := make([]byte, count)
	n, err := r.ReadAt(buf, int64(offset))
	if err == io.EOF {
		err = nil
	}
	return buf[:n], err
}

func (e *epubFile) Close(fid uint64) error {
	e.Lock()
	defer e.Unlock()
	if r, ok := e.fids[fid]; ok {
		r.Close()
		delete(e.fids, fid)
	}
	return nil
}
