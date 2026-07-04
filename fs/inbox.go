package fs

import (
	"errors"
	"log"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
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
	fid      uint64
	staged   *library.StagedFile
	lib      library.Library
	onIngest func(*model.Book)
}

func inboxCreateFile(lib library.Library, onIngest func(*model.Book)) createFileFunc {
	return func(f *fs.FS, parent fs.Dir, user, name string, perm uint32, mode uint8) (fs.File, error) {
		log.Printf("inbox: create %q perm=%o mode=%d parent=%s", name, perm, mode, fs.FullPath(parent))
		var inbox *inboxDir
		switch p := parent.(type) {
		case *inboxDir:
			inbox = p
		default:
			return nil, errors.New("not under inbox")
		}

		file := newInboxFile(f, lib, name, perm, onIngest)
		inbox.DeleteChild(name)
		if err := inbox.AddChild(file); err != nil {
			log.Printf("inbox: AddChild %q: %v", name, err)
			return nil, err
		}
		return file, nil
	}
}

func newInboxFile(f *fs.FS, lib library.Library, name string, perm uint32, onIngest func(*model.Book)) *inboxFile {
	return &inboxFile{
		BaseFile:  *fs.NewBaseFile(f.NewStat(name, "glenda", "glenda", perm)),
		lib:       lib,
		onIngest:  onIngest,
	}
}

func (i *inboxFile) Open(fid uint64, omode proto.Mode) error {
	log.Printf("inbox: open %q fid=%d omode=%d", i.Stat().Name, fid, omode)
	i.Lock()
	defer i.Unlock()
	if i.fid != 0 {
		log.Printf("already open")
		return errors.New("file already open")
	}

	sf, err := i.lib.CreateTemp()
	if err != nil {
		log.Printf("inbox: open %q: %v", i.Stat().Name, err)
		return err
	}
	i.staged = sf
	i.fid = fid

	return nil
}

func (i *inboxFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	i.Lock()
	defer i.Unlock()
	if i.staged == nil || i.fid != fid {
		log.Printf("inbox: write file was not opened")
		return 0, errors.New("file not opened with this fid")
	}

	n, err := i.staged.WriteAt(data, int64(offset))

	return uint32(n), err
}

// teardown releases the staged file under the lock. The caller must not
// hold the lock when calling DeleteChild or Ingest, since those re-enter
// the mutex via SetParent.
func (i *inboxFile) teardown() *library.StagedFile {
	i.Lock()
	defer i.Unlock()
	sf := i.staged
	i.staged = nil
	i.fid = 0
	return sf
}

func (i *inboxFile) Close(fid uint64) error {
	log.Printf("inbox: close %q fid=%d", i.Stat().Name, fid)

	sf := i.teardown()
	if sf == nil {
		return nil
	}

	parent := i.Parent()
	if md, ok := parent.(fs.ModDir); ok {
		md.DeleteChild(i.Stat().Name)
	}

	b, err := i.lib.Ingest(sf)
	if err != nil {
		log.Printf("inbox: ingest %q: %v", i.Stat().Name, err)
		return err
	}
	i.onIngest(b)
	log.Printf("inbox: ingested %q as book %d", i.Stat().Name, b.Meta.ID)
	return nil
}
