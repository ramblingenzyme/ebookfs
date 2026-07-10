package views

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/library"
)

// readerDir is the reader/ export view: the books whose status is in the
// configured set, grouped by author, served through the injected Exporter. It
// mirrors byAuthorDir, but its leaves are export files rather than bookDirs and
// it files each book under a single folder named for all its authors — so a
// co-authored book is exported once, not duplicated under each author.
type readerDir struct {
	groupingDir
	exp      library.Exporter
	included map[string]bool
}

func NewReaderDir(reg *registry.BookRegistry, exp library.Exporter) *readerDir {
	included := make(map[string]bool)
	for _, s := range exp.Statuses() {
		included[s] = true
	}
	d := &readerDir{
		groupingDir: newGroupingDir(reg.FS(), "reader"),
		exp:         exp,
		included:    included,
	}
	reg.AddView(d)
	return d
}

// authorDir returns the subdir for an author name, creating it on first use.
func (d *readerDir) authorDir(name string) fs.ModDir {
	return d.childDir(name, func(s *proto.Stat) fs.FSNode { return fs.NewStaticDir(s) }).(fs.ModDir)
}

func (d *readerDir) Add(dir *book.BookDir) {
	b := dir.Book()
	if !d.included[b.Meta.Status] {
		return
	}
	ad := d.authorDir(d.exp.Dirname(b))
	stat := newStat(d.f, d.exp.Filename(b), 0444)
	ad.AddChild(book.NewReaderFile(stat, d.exp, dir.Book))
	d.exp.Warm(b)
}

func (d *readerDir) Remove(dir *book.BookDir) {
	b := dir.Book()
	if !d.included[b.Meta.Status] {
		return
	}
	name := d.exp.Dirname(b)
	if child, ok := d.Children()[name]; ok {
		child.(fs.ModDir).DeleteChild(d.exp.Filename(b))
		d.pruneEmpty(name)
	}
}
