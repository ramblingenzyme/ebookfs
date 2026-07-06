package fs

import (
	"fmt"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
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

func (d *booksDir) add(dir *bookDir) {
	b := dir.Book()
	plain := b.Title
	if child, ok := d.Children()[plain]; ok && child != dir {
		// Plain title is taken by a different book — disambiguate with the id.
		d.AddChild(&namedBookDir{
			bookDir:  dir,
			baseStat: dir.Stat(),
			name: func(b *model.Book) string {
				return fmt.Sprintf("%s (%d)", b.Title, b.Meta.ID)
			},
		})
		return
	}
	d.AddChild(dir)
}

func (d *booksDir) remove(dir *bookDir) {
	b := dir.Book()
	plain := b.Title
	if child, ok := d.Children()[plain]; ok && child == dir {
		d.DeleteChild(plain)
		return
	}
	d.DeleteChild(fmt.Sprintf("%s (%d)", plain, b.Meta.ID))
}

func newAllBooksDir(reg *bookRegistry) *booksDir {
	d := newBooksDir(reg.f.NewStat("books", "glenda", "glenda", 0555|proto.DMDIR))
	reg.AddView(d)
	return d
}
