// Package inbox implements the write-only inbox/ directory. A client creating
// and writing a file there streams it through the library's ingest handle, and
// on close the book is ingested and handed to the registry.
package inbox

import (
	"log/slog"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type Ingester interface {
	CreateIngest() (library.IngestHandle, error)
}

var newStat = vfile.NewStat

// creatableDir is the mode inbox alone carries; vfile says why.
const creatableDir = vfile.CreatableDirMode

type InboxDir struct {
	fs.StaticDir
	lib      Ingester
	onIngest func(*library.Book)
}

func NewInboxDir(f *fs.FS, lib Ingester, onIngest func(*library.Book)) *InboxDir {
	return &InboxDir{
		StaticDir: *fs.NewStaticDir(newStat(f, "inbox", creatableDir)),
		lib:       lib,
		onIngest:  onIngest,
	}
}

// Create satisfies vfile.Creator. The FS-wide DispatchCreate hook routes
// creation here, so this package owns its own policy and not the tree's.
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
