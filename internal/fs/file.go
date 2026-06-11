package fs

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// coverFile serves a book's cover image, loading bytes from the epub on each open.
type coverFile struct {
	fs.BaseFile
	lib  *library.Library
	book *model.Book
	fids map[uint64][]byte
}

func newCoverFile(stat *proto.Stat, lib *library.Library, book *model.Book) *coverFile {
	return &coverFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		fids:     make(map[uint64][]byte),
	}
}

func (c *coverFile) Open(fid uint64, omode proto.Mode) error {
	data, err := c.lib.ExtractCover(c.book)
	if err != nil {
		return err
	}
	c.Lock()
	c.fids[fid] = data
	c.Unlock()
	return nil
}

func (c *coverFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	c.RLock()
	data := c.fids[fid]
	c.RUnlock()
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

func (c *coverFile) Close(fid uint64) error {
	c.Lock()
	delete(c.fids, fid)
	c.Unlock()
	return nil
}

// epubFile serves a book's epub through the library, holding one reader per fid.
// Size is statted once at construction; content is read on demand via ReadAt.
// The 9P layer never sees a filesystem path.
type epubFile struct {
	fs.BaseFile
	lib  *library.Library
	book *model.Book
	fids map[uint64]library.EpubReader
}

func newEpubFile(stat *proto.Stat, lib *library.Library, book *model.Book) *epubFile {
	// Stat the epub once here to fill in the 9P length; reads use per-fid handles.
	//
	// TODO: this length is captured at construction. When the ebook-meta edit path
	// rewrites an epub in place its size changes, so the book's fs node must be
	// rebuilt (or this stat refreshed) for clients to see the new length.
	if r, err := lib.OpenEpub(book); err == nil {
		if fi, err := r.Stat(); err == nil {
			stat.Length = uint64(fi.Size())
		}
		r.Close()
	}
	return &epubFile{
		BaseFile: *fs.NewBaseFile(stat),
		lib:      lib,
		book:     book,
		fids:     make(map[uint64]library.EpubReader),
	}
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
	r := e.fids[fid]
	e.RUnlock()
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
