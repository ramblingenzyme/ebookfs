package views

import (
	"sort"

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
// This keeps every book's *book.BookDir in memory and re-sorts the whole
// slice on each Add/Remove, which is fine at personal-library scale but
// wasteful for a very large one. If that ever matters, an alternative is to
// drop the in-memory population and instead re-run the existing
// model.Filter{Recent: true, Limit: recentLimit} query against the index on
// each mutation — trading an in-process resort for a small SQL query, the
// same tradeoff the stats file already makes (see statsFile.Stat).
type recentDir struct {
	*bookListDir
	all     []*book.BookDir         // every known book
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
	d.all = append(d.all, dir)
	d.refresh()
}

func (d *recentDir) Remove(dir *book.BookDir) {
	id := dir.Book().Meta.ID
	for i, bd := range d.all {
		if bd.Book().Meta.ID == id {
			d.all = append(d.all[:i], d.all[i+1:]...)
			break
		}
	}
	d.refresh()
}

// refresh re-sorts the full population by DateAdded (newest first) and
// reconciles the underlying bookListDir with whichever recentLimit books now
// rank highest, adding newly-visible entries and removing ones that fell out.
func (d *recentDir) refresh() {
	sort.Slice(d.all, func(i, j int) bool {
		return d.all[i].Book().Meta.DateAdded.After(d.all[j].Book().Meta.DateAdded)
	})

	top := d.all
	if len(top) > recentLimit {
		top = top[:recentLimit]
	}

	now := make(map[int64]*book.BookDir, len(top))
	for _, dir := range top {
		id := dir.Book().Meta.ID
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
