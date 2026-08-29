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
	"slices"
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/library"
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

	// editMu serializes edits per book id across the whole lib.Edit + commit
	// span, so snapshot swaps land in the same order as the library's writes.
	editMu syncutil.KeyedMutex
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

// ResyncView atomically rebuilds v's membership: reset runs first (typically
// clearing v and swapping its filter), then every registered book is offered to
// v.Add, all under the registry lock so the rebuild is serialized against
// Add/Remove/commit notifications. v decides membership inside its Add, so a
// view that attached after books existed — or changed its filter — converges on
// the registry's current state with no window for lost or stale entries. reset
// must not call back into the registry or the library.
func (r *BookRegistry) ResyncView(v BookView, reset func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	reset()
	for _, dir := range r.books {
		v.Add(dir)
	}
}

// RemoveView unregisters v from receiving Add/Remove for every book.
func (r *BookRegistry) RemoveView(v BookView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.views, v); i >= 0 {
		r.views = slices.Delete(r.views, i, i+1)
	}
}

// AddView registers v to receive Add/Remove for every book. Register all views
// before adding books.
func (r *BookRegistry) AddView(v BookView) {
	r.mu.Lock()
	r.views = append(r.views, v)
	r.mu.Unlock()
}

func (r *BookRegistry) dirLocked(bk *library.Book) *book.BookDir {
	if d, ok := r.books[bk.ID()]; ok {
		return d
	}
	d := book.NewBookDir(r.f, r.lib, r.Edit, bk)
	r.books[bk.ID()] = d
	return d
}

// commit brackets a snapshot swap with view removal and re-addition, so every
// view drops the book from its old slot (reading the old snapshot) and re-files
// it under the new one (reading the new snapshot). Callers hold r.mu and must
// persist before calling, so a failed write never reaches the tree. This is the
// shared primitive for meta and bib edits.
func (r *BookRegistry) commit(dir *book.BookDir, updated *library.Book) {
	for _, v := range r.views {
		v.Remove(dir)
	}
	dir.SetSnapshot(updated)
	for _, v := range r.views {
		v.Add(dir)
	}
}

// Add registers a newly ingested book and files it into every view.
func (r *BookRegistry) Add(book *library.Book) {
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

// Edit persists edits to a book and commits the change so views rehome the
// book if its grouping or name changed.
//
// lib.Edit can rewrite the whole epub (seconds of disk I/O), so it must not
// run under r.mu — that would queue every unrelated mutation behind it. The
// per-book editMu keeps concurrent edits of the same book (and their commits)
// in order; r.mu is only held for the lookup and the commit bracket.
func (r *BookRegistry) Edit(id int64, edits library.Edits) error {
	mu := r.editMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	r.mu.RLock()
	dir, ok := r.books[id]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("book %d: %w", id, library.ErrBookNotFound)
	}

	updated, err := r.lib.Edit(id, edits)
	if err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.books[id] != dir {
		// The book was removed while the edit ran; the disk write stands, but
		// there is no tree entry left to re-file.
		return nil
	}
	r.commit(dir, updated)
	return nil
}
