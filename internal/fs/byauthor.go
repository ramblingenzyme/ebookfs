package fs

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

type byAuthorDir struct{ groupingDir }

// TODO: when the OPF write path lands and authors become writable, add a
// remove/rehome path: look up the old author dirs, remove the book, then call
// add with the updated book so it lands under the new author(s).
func newByAuthorDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *byAuthorDir {
	d := &byAuthorDir{newGroupingDir(f, reg, "by-author")}
	for _, book := range books {
		d.add(book)
	}
	return d
}

func (d *byAuthorDir) add(book *model.Book) {
	for _, a := range book.Authors {
		key := a.Name
		var authorDir *booksDir
		if child, ok := d.Children()[key]; ok {
			authorDir = child.(*booksDir)
		} else {
			authorDir = newBooksDir(
				d.f.NewStat(key, "glenda", "glenda", 0555|proto.DMDIR),
				d.reg,
				nil,
			)
			d.StaticDir.AddChild(authorDir)
		}
		authorDir.add(book)
	}
}
