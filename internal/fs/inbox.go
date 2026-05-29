package fs

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/epub"
	"github.com/ramblingenzyme/ebookfs/internal/store"
)

type inboxDir struct {
	fs.StaticDir
}

func newInboxDir(f *fs.FS) *inboxDir {
	return &inboxDir{
		StaticDir: *fs.NewStaticDir(f.NewStat("inbox", "glenda", "glenda", 0755|proto.DMDIR)),
	}
}

type inboxFile struct {
	fs.BaseFile
	path string
	fid  uint64
	f    *os.File
	lib  *store.Library
}

func newInboxFile(lib *store.Library, inboxTemp, name string) *inboxFile {
	return &inboxFile{
		lib:  lib,
		path: filepath.Join(inboxTemp, name),
	}
}

func (i *inboxFile) Open(fid uint64, omode proto.Mode) error {
	if i.fid != 0 {
		return errors.New("file already open")
	}

	i.fid = fid

	f, err := os.Create(i.path)
	if err != nil {
		return err
	}
	i.f = f

	return nil
}

func (i *inboxFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	if i.f == nil || i.fid != fid {
		return 0, errors.New("wtf")
	}

	n, err := i.f.WriteAt(data, int64(offset))

	return uint32(n), err
}

func (i *inboxFile) Close(fid uint64) error {
	i.f.Close()

	book, err := epub.Parse(i.path)
	if err != nil {
		os.Remove(i.path)
		return err
	}

	id := i.lib.AllocateID()

	// TODO: wrap in a SQLite transaction once the index exists — insert the
	// StoredBook into the index in the same transaction as Ingest.
	_, err = i.lib.Ingest(id, book, i.path)
	return err
}
