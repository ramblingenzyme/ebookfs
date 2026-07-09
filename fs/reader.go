package fs

import (
	"errors"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
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

func newReaderDir(reg *bookRegistry, exp library.Exporter) *readerDir {
	included := make(map[string]bool)
	for _, s := range exp.Statuses() {
		included[s] = true
	}
	d := &readerDir{
		groupingDir: newGroupingDir(reg.f, "reader"),
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

func (d *readerDir) add(dir *bookDir) {
	b := dir.Book()
	if !d.included[b.Meta.Status] {
		return
	}
	ad := d.authorDir(d.exp.Dirname(b))
	stat := newStat(d.f, d.exp.Filename(b), 0444)
	ad.AddChild(newReaderFile(stat, d.exp, dir.Book))
	d.exp.Warm(b)
}

func (d *readerDir) remove(dir *bookDir) {
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

// readerFile serves a book's export rendition through the Exporter, holding one
// reader per fid. It mirrors epubFile, but its size is reported live from the
// exporter so a kepub's length appears once its cache is warm.
type readerFile struct {
	readAtFile
	exp  library.Exporter
	book func() *model.Book
}

func newReaderFile(stat *proto.Stat, exp library.Exporter, book func() *model.Book) *readerFile {
	return &readerFile{
		readAtFile: newReadAtFile(stat, func() (library.EpubReader, error) {
			if exp == nil {
				return nil, errors.New("exporter not available")
			}
			return exp.Open(book())
		}),
		exp:  exp,
		book: book,
	}
}

// Stat reports the export size when known (cheap; never triggers a conversion),
// so a cold kepub lists as length 0 until its cache is warm.
func (r *readerFile) Stat() proto.Stat {
	s := r.BaseFile.Stat()
	if size, ok := r.exp.Size(r.book()); ok {
		s.Length = uint64(size)
	}
	return s
}
