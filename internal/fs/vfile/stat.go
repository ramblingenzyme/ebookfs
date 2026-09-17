package vfile

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// NewStat builds a proto.Stat owned by the conventional glenda/glenda
// user/group, the single owner every node in the served tree uses. Callers
// supply only the name and mode (OR in proto.DMDIR for directories).
func NewStat(f *fs.FS, name string, mode uint32) *proto.Stat {
	return f.NewStat(name, "glenda", "glenda", mode)
}
