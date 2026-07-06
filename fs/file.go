package fs

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// opfFile serves a book's raw OPF XML, loading bytes from the epub on each open.
// book is a getter (bookDir.Book) so every access sees the current snapshot.
type opfFile struct {
	fs.BaseFile
	lib   library.Library
	book  func() *model.Book
	reads map[uint64][]byte
}

func newOPFFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *opfFile {
	return &opfFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		reads:    make(map[uint64][]byte),
	}
}

func (o *opfFile) Stat() proto.Stat {
	s := o.BaseFile.Stat()
	if o.lib != nil {
		if data, err := o.lib.ExtractOPF(o.book()); err == nil {
			s.Length = uint64(len(data))
		}
	}
	return s
}

func (o *opfFile) Open(fid uint64, omode proto.Mode) error {
	data, err := o.lib.ExtractOPF(o.book())
	if err != nil {
		return err
	}
	o.Lock()
	o.reads[fid] = data
	o.Unlock()
	return nil
}

func (o *opfFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	o.RLock()
	defer o.RUnlock()
	data := o.reads[fid]
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

func (o *opfFile) Close(fid uint64) error {
	o.Lock()
	defer o.Unlock()
	delete(o.reads, fid)
	return nil
}

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	fs.BaseFile
	lib    library.Library
	book   func() *model.Book
	reads  map[uint64][]byte
	writes map[uint64][]byte
}

func newCoverFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *coverFile {
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
		if data, err := c.lib.ExtractCover(c.book()); err == nil {
			s.Length = uint64(len(data))
		}
	}
	return s
}

func (c *coverFile) Open(fid uint64, omode proto.Mode) error {
	data, err := c.lib.ExtractCover(c.book())
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
	return c.lib.WriteCover(c.book().Meta.ID, data)
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// The 9P layer never sees a filesystem path. Stat is live: it reports the
// current EpubFilename and on-disk size on each call.
type epubFile struct {
	fs.BaseFile
	lib  library.Library
	book func() *model.Book
	fids map[uint64]library.EpubReader
}

func newEpubFile(stat *proto.Stat, lib library.Library, book func() *model.Book) *epubFile {
	return &epubFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		fids:     make(map[uint64]library.EpubReader),
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

func (e *epubFile) Open(fid uint64, omode proto.Mode) error {
	r, err := e.lib.OpenEpub(e.book())
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
