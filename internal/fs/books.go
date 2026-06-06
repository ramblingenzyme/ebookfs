package fs

import (
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/library"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

type allBooksDir struct {
	booksDir
}

func newAllBooksDir(f *fs.FS, reg *bookRegistry, books []*model.Book) *allBooksDir {
	return &allBooksDir{
		booksDir: *newBooksDir(f.NewStat("books", "glenda", "glenda", 0555|proto.DMDIR), reg, books),
	}
}

// bookRegistry ensures at most one bookDir exists per book ID. Virtual and
// search directories hold pointers into this registry rather than owning their
// own copies, so mutations (rating, status, tags) are visible everywhere.
type bookRegistry struct {
	mu    sync.RWMutex
	books map[int64]*bookDir
	f     *fs.FS
	lib   *library.Library
}

func newBookRegistry(f *fs.FS, lib *library.Library) *bookRegistry {
	return &bookRegistry{
		books: make(map[int64]*bookDir),
		f:     f,
		lib:   lib,
	}
}

func (r *bookRegistry) getOrCreate(book *model.Book) *bookDir {
	r.mu.Lock()
	defer r.mu.Unlock()
	if d, ok := r.books[book.Meta.ID]; ok {
		return d
	}
	d := newBookDir(r.f, r.lib, book)
	r.books[book.Meta.ID] = d
	return d
}

func (r *bookRegistry) get(id int64) (*bookDir, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.books[id]
	return d, ok
}

func (r *bookRegistry) remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.books, id)
}

// booksDir is a directory whose children are bookDirs drawn from the registry.
// Embed it in view-specific types (all books, by-author, search results, etc.)
// and call add/remove to manage the listing.
type booksDir struct {
	fs.StaticDir
	registry *bookRegistry
}

func newBooksDir(stat *proto.Stat, reg *bookRegistry, books []*model.Book) *booksDir {
	d := &booksDir{
		StaticDir: *fs.NewStaticDir(stat),
		registry:  reg,
	}
	for _, book := range books {
		d.StaticDir.AddChild(reg.getOrCreate(book))
	}
	return d
}

func (d *booksDir) add(book *model.Book) {
	d.StaticDir.AddChild(d.registry.getOrCreate(book))
}

func (d *booksDir) remove(id int64) {
	if bd, ok := d.registry.get(id); ok {
		d.StaticDir.DeleteChild(bd.Stat().Name)
	}
}
