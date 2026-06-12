package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// namedBookDir wraps a shared *bookDir with a custom stat, used when a listing
// presents a book under a name other than its title (by-id, by-series).
type namedBookDir struct {
	*bookDir
	entryStat proto.Stat
}

func (n *namedBookDir) Stat() proto.Stat { return n.entryStat }

// groupingDir is the shared base for by-x view directories.
type groupingDir struct {
	fs.StaticDir
	f   *fs.FS
	reg *bookRegistry
}

func newGroupingDir(f *fs.FS, reg *bookRegistry, name string) groupingDir {
	return groupingDir{
		StaticDir: *fs.NewStaticDir(f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)),
		f:         f,
		reg:       reg,
	}
}

