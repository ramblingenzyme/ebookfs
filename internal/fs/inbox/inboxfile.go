package inbox

import (
	"errors"
	"log/slog"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type InboxFile struct {
	fs.BaseFile
	fid      uint64
	handle   library.IngestHandle
	lib      Ingester
	onIngest func(*library.Book)
}

func NewInboxFile(f *fs.FS, lib Ingester, name string, perm uint32, onIngest func(*library.Book)) *InboxFile {
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
	slog.Info("inbox: ingested", "name", i.Stat().Name, "book_id", b.ID())
	return nil
}
