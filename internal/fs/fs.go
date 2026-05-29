package fs

import (
	"errors"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/store"
)

func newFS(lib *store.Library, inboxTemp string) (*fs.FS, *fs.StaticDir) {
	createFile := func(f *fs.FS, parent fs.Dir, user, name string, perm uint32, mode uint8) (fs.File, error) {
		if fs.FullPath(parent) != "/inbox" {
			return nil, errors.New("not under inbox")
		}
		return newInboxFile(lib, inboxTemp, name), nil
	}

	return fs.NewFS("glenda", "glenda", 0555, fs.WithCreateFile(createFile))
}
