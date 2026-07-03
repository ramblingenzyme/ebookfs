package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// booksDir is a flat listing of bookDirs keyed by each book's title. Embed it in
// views that present an unordered set of books (all books, one author's books,
// search results).
type booksDir struct {
	fs.StaticDir
}

func newBooksDir(stat *proto.Stat) *booksDir {
	return &booksDir{StaticDir: *fs.NewStaticDir(stat)}
}

func (d *booksDir) add(dir *bookDir)    { d.StaticDir.AddChild(dir) }
func (d *booksDir) remove(dir *bookDir) { d.StaticDir.DeleteChild(dir.Stat().Name) }

func newAllBooksDir(reg *bookRegistry) *booksDir {
	d := newBooksDir(reg.f.NewStat("books", "glenda", "glenda", 0555|proto.DMDIR))
	reg.AddView(d)
	return d
}
