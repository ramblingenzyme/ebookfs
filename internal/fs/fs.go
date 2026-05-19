package fs

import (
	"errors"

	"github.com/knusbaum/go9p/fs"

)


var (
	ebookfs *fs.FS
	root *fs.StaticDir
)

func createFile(f *fs.FS, parent fs.Dir, user, name string, perm uint32, mode uint8) (fs.File, error) {
	if fs.FullPath(parent) != "/inbox" {
		return nil, errors.New("not under inbox")
	}

	return newInboxFile(), nil
}

func getFS() (*fs.FS, *fs.StaticDir) {
	if ebookfs == nil {
		ebookfs, root = fs.NewFS("glenda", "glenda", 0555, fs.WithCreateFile(createFile))
	}

	return ebookfs, root
}
