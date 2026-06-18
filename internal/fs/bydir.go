package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// namedBookDir wraps a shared *bookDir to present it under a name other than its
// title (by-id, by-series). The name is recomputed live from the book, so a
// title or series-index edit is reflected without rebuilding the entry; baseStat
// carries a stable Qid distinct from the bare bookDir's listing.
type namedBookDir struct {
	*bookDir
	baseStat proto.Stat
	name     func(*model.Book) string
}

func (n *namedBookDir) Stat() proto.Stat {
	s := n.baseStat
	s.Name = n.name(n.bookDir.Book)
	return s
}

// groupingDir is the shared base for by-x view directories.
type groupingDir struct {
	fs.StaticDir
	f *fs.FS
}

func newGroupingDir(f *fs.FS, name string) groupingDir {
	return groupingDir{
		StaticDir: *fs.NewStaticDir(f.NewStat(name, "glenda", "glenda", 0555|proto.DMDIR)),
		f:         f,
	}
}
