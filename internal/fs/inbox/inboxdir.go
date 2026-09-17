// Package inbox implements the write-only inbox/ directory of the served tree:
// a client creating and writing a file there streams it through the library's
// ingest handle, and on close the book is ingested and handed to the registry.
// It depends only on the library facade and the vfile stat convention, not on
// the book directory tree, so it is a leaf of the frontend.
package inbox

import (
	"log/slog"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/library"
)

// Ingester is the half of the library this package uses: an upload is a staged
// handle the client writes into.
type Ingester interface {
	CreateIngest() (library.IngestHandle, error)
}

// newStat is the package-local shorthand for vfile.NewStat, the single
// definition of the glenda/glenda owner convention every node uses.
var newStat = vfile.NewStat

type InboxDir struct {
	fs.StaticDir
	lib      Ingester
	onIngest func(*library.Book)
}

func NewInboxDir(f *fs.FS, lib Ingester, onIngest func(*library.Book)) *InboxDir {
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
