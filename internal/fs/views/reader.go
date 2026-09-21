package views

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// readerDir is the reader/ export view: books whose status is in the configured
// set, served through the Exporter. Its leaves are export files rather than
// bookDirs, and each book sits under one folder named for all its authors, so a
// co-authored book is exported once.
type readerDir struct {
	groupingDir
	// Not narrowed: the view uses four of the six and hands the rest to the
	// ReaderFile it builds, so a local interface would restate the contract.
	exp library.Exporter
}

func NewReaderDir(reg *registry.BookRegistry, exp library.Exporter) *readerDir {
	d := &readerDir{
		groupingDir: newGroupingDir(reg.FS(), "reader"),
		exp:         exp,
	}
	reg.AddView(d)
	return d
}

func (d *readerDir) authorDir(name string) fs.ModDir {
	return d.childDir(name, func(s *proto.Stat) fs.FSNode { return fs.NewStaticDir(s) }).(fs.ModDir)
}

func (d *readerDir) Add(dir *book.BookDir) {
	b := dir.Book()
	if !d.exp.Includes(b) {
		return
	}
	ad := d.authorDir(d.exp.Dirname(b))
	stat := newStat(d.f, d.exp.Filename(b), 0444)
	ad.AddChild(book.NewReaderFile(stat, d.exp, dir.Book))
	d.exp.Warm(b)
}

func (d *readerDir) Remove(dir *book.BookDir) {
	b := dir.Book()
	if !d.exp.Includes(b) {
		return
	}
	name := d.exp.Dirname(b)
	if child, ok := d.Children()[name]; ok {
		child.(fs.ModDir).DeleteChild(d.exp.Filename(b))
		d.pruneEmpty(name)
	}
}
