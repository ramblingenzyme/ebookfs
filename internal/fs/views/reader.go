package views

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ReaderExporter is what the reader view needs of an exporter. Four of the six
// are its own; book.Renderer covers the two it hands to the ReaderFile it
// constructs, which is why this one cannot narrow further.
type ReaderExporter interface {
	book.Renderer
	Includes(*library.Book) bool  // whether the book appears in this view
	Dirname(*library.Book) string // FAT-safe export directory name
	Filename(*library.Book) string
	Warm(*library.Book) // non-blocking proactive warm hint
}

// readerDir is the reader/ export view: the books whose status is in the
// configured set, grouped by author, served through the injected Exporter. It
// mirrors byAuthorDir, but its leaves are export files rather than bookDirs and
// it files each book under a single folder named for all its authors — so a
// co-authored book is exported once, not duplicated under each author.
type readerDir struct {
	groupingDir
	exp ReaderExporter
}

func NewReaderDir(reg *registry.BookRegistry, exp ReaderExporter) *readerDir {
	d := &readerDir{
		groupingDir: newGroupingDir(reg.FS(), "reader"),
		exp:         exp,
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
