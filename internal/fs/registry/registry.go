// Package registry holds the BookRegistry. It knows views only through the
// BookView interface, so a view never sees the snapshot swap and a change to
// presentation cannot reorder it.
package registry

import (
	"os"
	"slices"
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Editor supplies the content reader and edit operation installed in each
// BookDir.
type Editor interface {
	book.ContentReader
	Edit(id int64, e library.Edits) (*library.Book, error)
}

// BookView is an FS listing that reacts to a book entering, leaving, or moving
// within it. The registry brackets snapshot replacement with Remove and Add,
// so each sees the old and new book respectively.
type BookView interface {
	Add(dir *book.BookDir)
	Remove(dir *book.BookDir)
}

// BookRegistry is the single authority on id → *book.BookDir and the
// orchestrator of every change to the served tree.
//
// A BookDir is a stable identity, so the map entry and any open fids survive
// an edit. Its book is an atomically swapped snapshot, since 9P handlers read
// it from many goroutines without taking r.mu.
type BookRegistry struct {
	library.HookBase

	mu      sync.RWMutex
	books   map[int64]*book.BookDir
	views   []BookView
	f       *fs.FS
	content book.ContentReader
	edit    func(int64, library.Edits) (*library.Book, error)
}

func NewBookRegistry(f *fs.FS, lib Editor) *BookRegistry {
	return &BookRegistry{
		books:   make(map[int64]*book.BookDir),
		f:       f,
		content: lib,
		edit:    lib.Edit,
	}
}

func (r *BookRegistry) FS() *fs.FS { return r.f }

// OnIngested implements library.Hook. It adds the book to the registry.
func (r *BookRegistry) OnIngested(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addLocked(book)
}

// OnEdited implements library.Hook. It replaces the snapshot and re-files the book in views.
func (r *BookRegistry) OnEdited(book *library.Book, sidecars *os.Root, openEpub func() (library.EpubReader, error)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[book.ID()]
	if !ok {
		return
	}
	r.commit(dir, book)
}

// OnDeleted implements library.Hook. It removes the book from the registry.
func (r *BookRegistry) OnDeleted(bookID int64) { r.remove(bookID) }

// AddView registers v and fills it with books already in the registry.
func (r *BookRegistry) AddView(v BookView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.views = append(r.views, v)
	for _, dir := range r.books {
		v.Add(dir)
	}
}

func (r *BookRegistry) RemoveView(v BookView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.views, v); i >= 0 {
		r.views = slices.Delete(r.views, i, i+1)
	}
}

// ResyncView rebuilds v's membership under the registry lock. The reset
// callback clears v before the registry replays its current books.
//
// reset must not call back into the registry or the library.
func (r *BookRegistry) ResyncView(v BookView, reset func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reset()
	for _, dir := range r.books {
		v.Add(dir)
	}
}

// Load seeds the registry with books already present when the frontend starts.
func (r *BookRegistry) Load(books ...*library.Book) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, bk := range books {
		r.addLocked(bk)
	}
}

func (r *BookRegistry) dirLocked(bk *library.Book) *book.BookDir {
	if d, ok := r.books[bk.ID()]; ok {
		return d
	}
	d := book.NewBookDir(r.f, r.content, r.edit, bk)
	r.books[bk.ID()] = d
	return d
}

func (r *BookRegistry) addLocked(bk *library.Book) {
	dir := r.dirLocked(bk)
	for _, v := range r.views {
		v.Add(dir)
	}
}

func (r *BookRegistry) remove(bookID int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir, ok := r.books[bookID]
	if !ok {
		return
	}
	for _, v := range r.views {
		v.Remove(dir)
	}
	delete(r.books, bookID)
}

// commit brackets a snapshot replacement with view removal and re-addition.
// Callers hold r.mu; each view sees the old snapshot on Remove and the new
// snapshot on Add.
func (r *BookRegistry) commit(dir *book.BookDir, updated *library.Book) {
	for _, v := range r.views {
		v.Remove(dir)
	}
	dir.SetSnapshot(updated)
	for _, v := range r.views {
		v.Add(dir)
	}
}
