package views

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

var (
	newStat    = vfile.NewStat
	newDirStat = vfile.NewDirStat
)

// namedBookDir wraps a shared *book.BookDir to present it under a name other
// than its title (by-id, by-series). The name is recomputed live from the book,
// so a title or series-index edit is reflected without rebuilding the entry.
type namedBookDir struct {
	*book.BookDir
	baseStat proto.Stat
	name     func(*library.Book) string
}

// newNamedBookDir presents dir under the name fn computes. The stat carries an
// empty name because Stat overwrites it on every call; what it is really for is
// the Qid, a fresh one distinct from the bare BookDir's, so the same book
// listed in two views is two entries.
func newNamedBookDir(f *fs.FS, dir *book.BookDir, fn func(*library.Book) string) *namedBookDir {
	return &namedBookDir{
		BookDir:  dir,
		baseStat: *newDirStat(f, ""),
		name:     fn,
	}
}

func (n *namedBookDir) Stat() proto.Stat {
	s := n.baseStat
	s.Name = n.name(n.BookDir.Book())
	return s
}

// groupingDir is the shared base for by-x view directories. StaticDir is a
// pointer, never a value, because fs.StaticDir embeds sync.RWMutex (via its
// BaseFile). Copying a mutex after first use is undefined behaviour, and
// groupingDir is returned by value and embedded by value in every view type
// (keyedDir, byIDDir, readerDir). A pointer avoids copying the mutex.
type groupingDir struct {
	*fs.StaticDir
	f *fs.FS
}

func newGroupingDir(f *fs.FS, name string) groupingDir {
	return groupingDir{
		StaticDir: fs.NewStaticDir(newDirStat(f, name)),
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
	ad := factory(newDirStat(g.f, name))
	g.StaticDir.AddChild(ad)
	return ad
}

// removeFromChild looks up the registry.BookView child named name, removes dir
// from it, and prunes the child if empty. It pairs with childDir.
func (g *groupingDir) removeFromChild(name string, dir *book.BookDir) {
	if child, ok := g.Children()[name]; ok {
		child.(registry.BookView).Remove(dir)
		g.pruneEmpty(name)
	}
}

// bookListFactory is the child a by-x view builds unless it needs a listing of
// its own, as by-series does.
func bookListFactory(s *proto.Stat) fs.FSNode { return newBookListDir(s) }

// keyedDir is a by-x view: one child per key the book yields, with the book
// filed into each. A view supplies the keys function and the child factory and
// has nothing else to say, which is why by-author, by-tag, by-status and
// by-series are all this type rather than four of their own.
//
// Add and Remove read the same keys through entryNames, so a book leaves
// exactly the entries it joined; a name minted on one side only makes removals
// miss.
//
// by-id and reader embed groupingDir directly instead, since neither files a
// book under a key the book carries.
type keyedDir struct {
	groupingDir
	// keys selects the values b belongs under, verbatim. entryNames turns them
	// into directory names, so a keys function never sanitizes.
	keys    func(*library.Book) []string
	factory func(*proto.Stat) fs.FSNode
}

func newKeyedDir(reg *registry.BookRegistry, name string, keys func(*library.Book) []string, factory func(*proto.Stat) fs.FSNode) *keyedDir {
	d := &keyedDir{
		groupingDir: newGroupingDir(reg.FS(), name),
		keys:        keys,
		factory:     factory,
	}
	reg.AddView(d)
	return d
}

// entryNames is the child directories b belongs under. Every group name in
// every by-x view is minted here, so a key that is metadata read verbatim from
// an epub cannot reach a listing as a name a 9P client is unable to walk to.
func (d *keyedDir) entryNames(b *library.Book) []string {
	keys := d.keys(b)
	names := make([]string, len(keys))
	for i, key := range keys {
		names[i] = naming.PathSafe(key)
	}
	return names
}

func (d *keyedDir) Add(dir *book.BookDir) {
	for _, name := range d.entryNames(dir.Book()) {
		d.childDir(name, d.factory).(registry.BookView).Add(dir)
	}
}

func (d *keyedDir) Remove(dir *book.BookDir) {
	for _, name := range d.entryNames(dir.Book()) {
		d.removeFromChild(name, dir)
	}
}
