package views

import (
	"fmt"

	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library"
)

// bookListDir is a flat listing of bookDirs keyed by each book's title. Embed it in
// views that present an unordered set of books (all books, one author's books,
// search results).
//
// entries records, per book id, the entry name Add minted (plain title or
// disambiguated), so Remove deletes exactly what was added instead of
// re-deriving the name and guessing which form Add chose. The registry
// serializes Add/Remove under its own lock, so the map needs none.
type bookListDir struct {
	fs.StaticDir
	entries map[int64]string
}

func newBookListDir(stat *proto.Stat) *bookListDir {
	return &bookListDir{
		StaticDir: *fs.NewStaticDir(stat),
		entries:   make(map[int64]string),
	}
}

// disambiguatedName is the entry name for a book whose plain title collides with
// another book's. Mirrors the store's canonicalDir convention.
//
// Known gap: Add tests the plain title for collisions but not the name it mints,
// so a book literally titled "Foo (2)" and the minted name for book 2 titled
// "Foo" are the same key. It only bites in one order — literal title added
// first, then the collision that mints over it: the minted entry replaces the
// literal one, so a registered book vanishes from the listing, and because
// entries then maps both ids to that name, removing either deletes the other's
// entry too. Added the other way round it is fine, since by then the plain
// title "Foo (2)" is taken and gets disambiguated in turn. Fixing it means
// minting until the name is free rather than assuming one pass suffices.
func disambiguatedName(b *library.Book) string {
	return fmt.Sprintf("%s (%d)", naming.PathSafe(b.Title()), b.ID())
}

func (d *bookListDir) Add(dir *book.BookDir) {
	b := dir.Book()
	// Keyed by the name the child actually carries: BookDir.Stat reports a
	// PathSafe title, so looking up the raw one would miss the collision and
	// entries would name a child that cannot be deleted.
	name := naming.PathSafe(b.Title())
	if child, ok := d.Children()[name]; ok && child != dir {
		// Plain title is taken by a different book — disambiguate with the id.
		d.AddChild(&namedBookDir{
			BookDir:  dir,
			baseStat: dir.Stat(),
			name:     disambiguatedName,
		})
		d.entries[b.ID()] = disambiguatedName(b)
		return
	}
	d.AddChild(dir)
	d.entries[b.ID()] = name
}

func (d *bookListDir) Remove(dir *book.BookDir) {
	id := dir.Book().ID()
	if name, ok := d.entries[id]; ok {
		d.DeleteChild(name)
		delete(d.entries, id)
	}
}

func (d *bookListDir) clear() {
	for id, name := range d.entries {
		d.DeleteChild(name)
		delete(d.entries, id)
	}
}

func NewAllBooksDir(reg *registry.BookRegistry) *bookListDir {
	d := newBookListDir(newStat(reg.FS(), "books", 0555|proto.DMDIR))
	reg.AddView(d)
	return d
}
