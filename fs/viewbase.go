package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
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
	s.Name = n.name(n.bookDir.Book())
	return s
}

// groupingDir is the shared base for by-x view directories. StaticDir is a
// pointer, never a value, because fs.StaticDir embeds sync.RWMutex (via its
// BaseFile). Copying a mutex after first use is undefined behaviour, and
// groupingDir is returned by value and embedded by value in every view type
// (byAuthorDir, bySeriesDir, readerDir). A pointer avoids copying the mutex.
type groupingDir struct {
	*fs.StaticDir
	f *fs.FS
}

func newGroupingDir(f *fs.FS, name string) groupingDir {
	return groupingDir{
		StaticDir: fs.NewStaticDir(newStat(f, name, 0555|proto.DMDIR)),
		f:         f,
	}
}

// pruneEmpty removes a child subdirectory if it exists and has no children of
// its own. Safe to call unconditionally after removing an entry from a subdir.
func (g *groupingDir) pruneEmpty(name string) {
	child, ok := g.Children()[name]
	if !ok {
		return
	}
	if dir, ok := child.(fs.Dir); ok && len(dir.Children()) == 0 {
		g.StaticDir.DeleteChild(name)
	}
}

// childDir returns the existing child with name, or creates one via factory and
// adds it. The factory receives a stat whose name is already set.
func (g *groupingDir) childDir(name string, factory func(*proto.Stat) fs.FSNode) fs.FSNode {
	if child, ok := g.Children()[name]; ok {
		return child
	}
	ad := factory(newStat(g.f, name, 0555|proto.DMDIR))
	g.StaticDir.AddChild(ad)
	return ad
}

// listerDir returns the bookView child named name, creating it via newBookListDir
// on first use.
func (g *groupingDir) listerDir(name string) bookView {
	return g.childDir(name, func(s *proto.Stat) fs.FSNode { return newBookListDir(s) }).(bookView)
}

// removeLister looks up the bookView child named name, removes dir from it,
// and prunes the child if empty.
func (g *groupingDir) removeLister(name string, dir *bookDir) {
	if child, ok := g.Children()[name]; ok {
		child.(bookView).remove(dir)
		g.pruneEmpty(name)
	}
}
