package vfile

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// The modes every synthesized directory is built with. The server runs with
// fs.IgnorePermissions, so these reach a client in an Rstat and gate nothing.
//
// The write bit is a claim about what the directory does rather than a
// permission. inbox is the only one that accepts a Tcreate; DispatchCreate
// answers the rest with "cannot create files here".
const (
	DirMode          = 0555 | proto.DMDIR
	CreatableDirMode = 0755 | proto.DMDIR
)

// NewStat builds a proto.Stat owned by glenda/glenda, the single owner every
// node in the served tree uses.
func NewStat(f *fs.FS, name string, mode uint32) *proto.Stat {
	return f.NewStat(name, "glenda", "glenda", mode)
}

// NewDirStat is NewStat for a listing a client cannot create in.
func NewDirStat(f *fs.FS, name string) *proto.Stat {
	return NewStat(f, name, DirMode)
}
