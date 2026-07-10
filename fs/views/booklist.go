package views

import (
	"fmt"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// bookListDir is a flat listing of bookDirs keyed by each book's title. Embed it in
// views that present an unordered set of books (all books, one author's books,
// search results).
type bookListDir struct {
	fs.StaticDir
}

func newBookListDir(stat *proto.Stat) *bookListDir {
	return &bookListDir{StaticDir: *fs.NewStaticDir(stat)}
}

func (d *bookListDir) Add(dir *book.BookDir) {
	b := dir.Book()
	plain := b.Title
	if child, ok := d.Children()[plain]; ok && child != dir {
		// Plain title is taken by a different book — disambiguate with the id.
		d.AddChild(&namedBookDir{
			BookDir:  dir,
			baseStat: dir.Stat(),
			name: func(b *model.Book) string {
				return fmt.Sprintf("%s (%d)", b.Title, b.Meta.ID)
			},
		})
		return
	}
	d.AddChild(dir)
}

func (d *bookListDir) Remove(dir *book.BookDir) {
	b := dir.Book()
	plain := b.Title
	if child, ok := d.Children()[plain]; ok && child == dir {
		d.DeleteChild(plain)
		return
	}
	d.DeleteChild(fmt.Sprintf("%s (%d)", plain, b.Meta.ID))
}

func NewAllBooksDir(reg *registry.BookRegistry) *bookListDir {
	d := newBookListDir(newStat(reg.FS(), "books", 0555|proto.DMDIR))
	reg.AddView(d)
	return d
}
