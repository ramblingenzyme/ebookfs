package views

import (
	"slices"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
)

// recentLimit is how many books recent/ shows, newest first.
const recentLimit = 5

// recentDir shows the recentLimit most-recently-added books. Unlike the by-x
// grouping views, membership depends on ranking against every other book
// rather than a fixed key on the book itself, and the registry calls
// Add/Remove for every book, not just the ones currently visible (see the
// initial-load loop in server.go and BookRegistry.Add/commit). So recentDir
// tracks the full population sorted by DateAdded, and reconciles the visible
// bookListDir against it, to know which book backfills a slot vacated by
// Remove.
//
// This keeps every book's *book.BookDir in memory, held in rank order: Add
// inserts at the right position rather than appending and re-sorting, so filing
// a whole library in at startup costs one insert per book instead of one sort
// of the population per book. The visible set is then just the head of the list.
//
// all and visible need no lock of their own: every path into Add/Remove holds
// BookRegistry.mu, and no 9P handler reads them. The embedded StaticDir's mutex
// guards the children listing instead, and AddChild/DeleteChild take it
// themselves — refresh must not hold it across them or it self-deadlocks.
type recentDir struct {
	*bookListDir
	all     []*book.BookDir         // every known book, newest first
	visible map[int64]*book.BookDir // ids currently filed into bookListDir
}

func NewRecentDir(reg *registry.BookRegistry) *recentDir {
	d := &recentDir{
		bookListDir: newBookListDir(newStat(reg.FS(), "recent", 0555|proto.DMDIR)),
		visible:     make(map[int64]*book.BookDir),
	}
	reg.AddView(d)
	return d
}

func (d *recentDir) Add(dir *book.BookDir) {
	i, _ := slices.BinarySearchFunc(d.all, dir, func(a, b *book.BookDir) int {
		return b.Book().DateAdded().Compare(a.Book().DateAdded()) // newest first
	})
	d.all = slices.Insert(d.all, i, dir)
	d.refresh()
}

func (d *recentDir) Remove(dir *book.BookDir) {
	id := dir.Book().ID()
	d.all = slices.DeleteFunc(d.all, func(bd *book.BookDir) bool {
		return bd.Book().ID() == id
	})
	d.refresh()
}

// refresh brings the underlying bookListDir in line with the recentLimit
// highest-ranked books, adding newly-visible entries and removing ones that
// fell out. all is kept in rank order by Add, so the top slice is a prefix.
func (d *recentDir) refresh() {
	top := d.all
	if len(top) > recentLimit {
		top = top[:recentLimit]
	}

	now := make(map[int64]*book.BookDir, len(top))
	for _, dir := range top {
		id := dir.Book().ID()
		now[id] = dir
		if _, ok := d.visible[id]; !ok {
			d.bookListDir.Add(dir)
		}
	}
	for id, dir := range d.visible {
		if _, ok := now[id]; !ok {
			d.bookListDir.Remove(dir)
		}
	}
	d.visible = now
}
