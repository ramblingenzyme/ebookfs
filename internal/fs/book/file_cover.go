package book

import (
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

const maxCoverFileSize = 32 << 20 // 32 MiB

// coverFile serves a book's cover image, loading bytes from the epub on each
// open. It also supports writing new cover bytes, accumulated per fid and
// committed when the fid is closed.
type coverFile struct {
	vfile.SnapshotFile
	edit   func(int64, library.Edits) error
	book   func() *library.Book
	writes vfile.WriteBuffer
}

func newCoverFile(stat *proto.Stat, lib ContentReader, edit func(int64, library.Edits) error, book func() *library.Book) *coverFile {
	return &coverFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, contentBytes(lib, book, library.EpubReader.Cover)),
		edit:         edit,
		book:         book,
		writes:       vfile.NewWriteBuffer(maxCoverFileSize),
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
	return c.edit(c.book().ID(), library.Edits{Cover: &data})
}
