// Package inbox implements the write-only inbox/ directory of the served tree:
// a client creating and writing a file there streams it through the library's
// ingest handle, and on close the book is ingested and handed to the registry.
// It depends only on the library facade and the vfile stat convention, not on
// the book directory tree, so it is a leaf of the frontend.
package inbox

import (
	"errors"
	"log/slog"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// newStat is the package-local shorthand for vfile.NewStat, the single
// definition of the glenda/glenda owner convention every node uses.
var newStat = vfile.NewStat

type InboxDir struct {
	fs.StaticDir
	lib      library.Library
	onIngest func(*model.Book)
}

func NewInboxDir(f *fs.FS, lib library.Library, onIngest func(*model.Book)) *InboxDir {
	return &InboxDir{
		StaticDir: *fs.NewStaticDir(newStat(f, "inbox", 0755|proto.DMDIR)),
		lib:       lib,
		onIngest:  onIngest,
	}
}

// Create satisfies vfile.Creator: a file created under the inbox is backed by
// a fresh InboxFile wired to the library and the ingest callback. The FS-wide
// vfile.DispatchCreate hook routes creates here, so this package owns only its
// own create behavior, not the whole tree's create policy.
func (d *InboxDir) Create(f *fs.FS, name string, perm uint32, mode uint8) (fs.File, error) {
	slog.Debug("inbox: create", "name", name, "perm", perm, "mode", mode)
	file := NewInboxFile(f, d.lib, name, perm, d.onIngest)
	d.DeleteChild(name)
	if err := d.AddChild(file); err != nil {
		slog.Error("inbox: AddChild failed", "name", name, "error", err)
		return nil, err
	}
	return file, nil
}

type InboxFile struct {
	fs.BaseFile
	fid      uint64
	handle   library.IngestHandle
	lib      library.Library
	onIngest func(*model.Book)
}

func NewInboxFile(f *fs.FS, lib library.Library, name string, perm uint32, onIngest func(*model.Book)) *InboxFile {
	return &InboxFile{
		BaseFile: *fs.NewBaseFile(newStat(f, name, perm)),
		lib:      lib,
		onIngest: onIngest,
	}
}

func (i *InboxFile) Open(fid uint64, omode proto.Mode) error {
	slog.Debug("inbox: open", "name", i.Stat().Name, "fid", fid, "omode", omode)
	name := i.Stat().Name // cache before Lock — Stat() acquires RLock, deadlocking if already write-locked
	i.Lock()
	defer i.Unlock()
	if i.handle != nil {
		slog.Warn("inbox: open on already-open file", "name", name)
		return errors.New("file already open")
	}

	h, err := i.lib.CreateIngest()
	if err != nil {
		slog.Error("inbox: open failed", "name", name, "error", err)
		return err
	}
	i.handle = h
	i.fid = fid

	return nil
}

func (i *InboxFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	i.Lock()
	defer i.Unlock()
	if i.handle == nil || i.fid != fid {
		slog.Warn("inbox: write on file that was not opened", "fid", fid)
		return 0, errors.New("file not opened with this fid")
	}

	n, err := i.handle.WriteAt(data, int64(offset))

	return uint32(n), err
}

// teardown releases the ingest handle under the lock. The caller must not
// hold the lock when calling DeleteChild or Ingest, since those re-enter
// the mutex via SetParent.
func (i *InboxFile) teardown() library.IngestHandle {
	i.Lock()
	defer i.Unlock()
	h := i.handle
	i.handle = nil
	i.fid = 0
	return h
}

func (i *InboxFile) Close(fid uint64) error {
	slog.Debug("inbox: close", "name", i.Stat().Name, "fid", fid)

	h := i.teardown()
	if h == nil {
		return nil
	}

	parent := i.Parent()
	if md, ok := parent.(fs.ModDir); ok {
		md.DeleteChild(i.Stat().Name)
	}

	b, err := h.Ingest()
	if err != nil {
		slog.Error("inbox: ingest failed", "name", i.Stat().Name, "error", err)
		return err
	}
	i.onIngest(b)
	slog.Info("inbox: ingested", "name", i.Stat().Name, "book_id", b.Meta.ID)
	return nil
}
