// Package registry holds the BookRegistry, the single authority over the served
// tree's id → *book.BookDir mapping and the orchestrator of every change to it.
//
// The registry is presentation-agnostic BY CONVENTION: it knows views only
// through the BookView interface and must never import fs/views or otherwise
// depend on how books are presented. Keeping this boundary lets the
// concurrency-critical snapshot-swap logic be reasoned about in isolation.
package registry

import (
	"fmt"
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// BookView is an FS listing that reacts to a book entering or leaving it. Add
// and Remove read the book's CURRENT state, so the registry brackets every
// mutation as Remove → mutate → Add: Remove sees the old grouping/name, Add
// sees the new one. There is no "update" — temporal ordering supplies old vs new.
type BookView interface {
	Add(dir *book.BookDir)
	Remove(dir *book.BookDir)
}

// BookRegistry is the single authority on id → *book.BookDir and the orchestrator
// of every change to the served tree. BookDirs are stable identities — the map
// entry and any open fids survive edits — while the book state inside each is an
// atomically swapped snapshot (see book.BookDir), because 9P handlers read it
// from many goroutines without taking r.mu.
type BookRegistry struct {
	mu    sync.RWMutex
	books map[int64]*book.BookDir
	views []BookView
	f     *fs.FS
	lib   library.Library
}

func NewBookRegistry(f *fs.FS, lib library.Library) *BookRegistry {
	return &BookRegistry{
		books: make(map[int64]*book.BookDir),
		f:     f,
		lib:   lib,
	}
}

// FS returns the filesystem the tree is served from, so views can build stats
// for the directories they create.
func (r *BookRegistry) FS() *fs.FS { return r.f }

// AddView registers v to receive Add/Remove for every book. Register all views
// before adding books.
func (r *BookRegistry) AddView(v BookView) {
	r.mu.Lock()
	r.views = append(r.views, v)
	r.mu.Unlock()
}

func (r *BookRegistry) dirLocked(bk *model.Book) *book.BookDir {
	if d, ok := r.books[bk.Meta.ID]; ok {
		return d
	}
	d := book.NewBookDir(r.f, r.lib, r.edit, bk)
	r.books[bk.Meta.ID] = d
	return d
}

// commit brackets a snapshot swap with view removal and re-addition, so every
// view drops the book from its old slot (reading the old snapshot) and re-files
// it under the new one (reading the new snapshot). Callers hold r.mu and must
// persist before calling, so a failed write never reaches the tree. This is the
// shared primitive for meta and bib edits.
func (r *BookRegistry) commit(dir *book.BookDir, updated *model.Book) {
	for _, v := range r.views {
		v.Remove(dir)
	}
	dir.SetSnapshot(updated)
	for _, v := range r.views {
		v.Add(dir)
	}
}

// Add registers a newly ingested book and files it into every view.
func (r *BookRegistry) Add(book *model.Book) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := r.dirLocked(book)
	for _, v := range r.views {
		v.Add(dir)
	}
}

// Remove drops a book from every view and forgets it.
func (r *BookRegistry) Remove(id int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[id]
	if !ok {
		return
	}
	for _, v := range r.views {
		v.Remove(dir)
	}
	delete(r.books, id)
}

// edit persists edits to a book and commits the change so views rehome the
// book if its grouping or name changed. It is unexported: the only production
// caller is a book's field file, which receives it as the edit callback passed
// to book.NewBookDir.
func (r *BookRegistry) edit(id int64, edits model.Edits) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[id]
	if !ok {
		return fmt.Errorf("no book with id %d", id)
	}
	updated, err := r.lib.Edit(id, edits)
	if err != nil {
		return err
	}
	r.commit(dir, updated)
	return nil
}
