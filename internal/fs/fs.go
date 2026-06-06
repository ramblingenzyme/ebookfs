package fs

import (
	"github.com/knusbaum/go9p/fs"
)

type createFileFunc func(*fs.FS, fs.Dir, string, string, uint32, uint8) (fs.File, error)

func newFS(createFile createFileFunc) (*fs.FS, *fs.StaticDir) {
	f, root := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions(), fs.WithCreateFile(createFile))
	return f, root
}
