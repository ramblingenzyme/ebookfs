package vfile

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// The modes every synthesized directory is built with. The server runs with
// fs.IgnorePermissions ("configures the server to not enforce user/group
// permission bits"), so these reach a client in an Rstat and gate nothing. The
// write bit is therefore a claim about what the directory does: it is set only
// where a Tcreate is accepted, so a listing does not advertise a create the
// server would refuse.
//
// Only inbox accepts one. DispatchCreate answers every other parent with
// "cannot create files here".
const (
	DirMode      = 0555 | proto.DMDIR
	CreatableDir = 0755 | proto.DMDIR
)

// NewStat builds a proto.Stat owned by the conventional glenda/glenda
// user/group, the single owner every node in the served tree uses. Callers
// supply only the name and mode.
func NewStat(f *fs.FS, name string, mode uint32) *proto.Stat {
	return f.NewStat(name, "glenda", "glenda", mode)
}

// NewDirStat is NewStat for a listing a client cannot create in, which is every
// directory in the tree but inbox.
func NewDirStat(f *fs.FS, name string) *proto.Stat {
	return NewStat(f, name, DirMode)
}
