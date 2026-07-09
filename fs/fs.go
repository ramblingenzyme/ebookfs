package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

type createFileFunc func(*fs.FS, fs.Dir, string, string, uint32, uint8) (fs.File, error)

func newFS() (*fs.FS, *fs.StaticDir) {
	f, root := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f, root
}

// newStat builds a proto.Stat owned by the conventional glenda/glenda
// user/group, the single owner every node in this tree uses. Callers supply
// only the name and mode (OR in proto.DMDIR for directories).
func newStat(f *fs.FS, name string, mode uint32) *proto.Stat {
	return f.NewStat(name, "glenda", "glenda", mode)
}
