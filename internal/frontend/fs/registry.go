package fs

import (
	"fmt"
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/backend/library"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// bookView is an FS listing that reacts to a book entering or leaving it. add
// and remove read the book's CURRENT state, so the registry brackets every
// mutation as remove → mutate → add: remove sees the old grouping/name, add
// sees the new one. There is no "update" — temporal ordering supplies old vs new.
type bookView interface {
	add(dir *bookDir)
	remove(dir *bookDir)
}

// bookRegistry is the single authority on id → *bookDir and the orchestrator of
// every change to the served tree. bookDirs are mutated in place rather than
// replaced, so the map identity and any open fids stay stable across edits.
type bookRegistry struct {
	mu    sync.RWMutex
	books map[int64]*bookDir
	views []bookView
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

// AddView registers v to receive add/remove for every book. Register all views
// before adding books.
func (r *bookRegistry) AddView(v bookView) {
	r.mu.Lock()
	r.views = append(r.views, v)
	r.mu.Unlock()
}

func (r *bookRegistry) dirLocked(book *model.Book) *bookDir {
	if d, ok := r.books[book.Meta.ID]; ok {
		return d
	}
	d := newBookDir(r, book)
	r.books[book.Meta.ID] = d
	return d
}

// commit brackets an in-place mutation with view removal and re-addition, so
// every view drops the book from its old slot and re-files it under the new one.
// Callers hold r.mu and must persist before calling, so a failed write never
// reaches the tree. This is the shared primitive for meta and (future) bib edits.
func (r *bookRegistry) commit(dir *bookDir, apply func()) {
	for _, v := range r.views {
		v.remove(dir)
	}
	apply()
	for _, v := range r.views {
		v.add(dir)
	}
}

// Add registers a newly ingested book and files it into every view.
func (r *bookRegistry) Add(book *model.Book) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := r.dirLocked(book)
	for _, v := range r.views {
		v.add(dir)
	}
}

// Remove drops a book from every view and forgets it.
func (r *bookRegistry) Remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[id]
	if !ok {
		return
	}
	for _, v := range r.views {
		v.remove(dir)
	}
	delete(r.books, id)
}

// edit persists edits to a book and commits the change so views rehome the
// book if its grouping or name changed.
func (r *bookRegistry) edit(id int64, edits model.Edits) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[id]
	if !ok {
		return fmt.Errorf("no book with id %d", id)
	}
	updated, err := r.lib.Edit(dir.Book, edits)
	if err != nil {
		return err
	}
	r.commit(dir, func() { *dir.Book = *updated })
	return nil
}
