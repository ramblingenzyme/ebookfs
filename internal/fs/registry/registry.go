// Package registry holds the BookRegistry. It knows views only through the
// BookView interface, so a view never sees the snapshot swap and a change to
// presentation cannot reorder it.
package registry

import (
	"fmt"
	"slices"
	"sync"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/util/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Editor is the half of the library this package uses. ContentReader is
// embedded because the node it re-renders reads the book's epub itself.
type Editor interface {
	book.ContentReader
	Edit(id int64, e library.Edits) (*library.Book, error)
}

// BookView is an FS listing that reacts to a book entering or leaving it. Add
// and Remove read the book's current state, so the registry brackets every
// mutation as Remove, mutate, Add. There is no "update"; the ordering carries
// it.
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
	mu    sync.RWMutex
	books map[int64]*book.BookDir
	views []BookView
	f     *fs.FS
	lib   Editor

	// editMu serializes edits per book id across the whole lib.Edit + commit
	// span, so snapshot swaps land in the same order as the library's writes.
	editMu syncutil.KeyedMutex
}

func NewBookRegistry(f *fs.FS, lib Editor) *BookRegistry {
	return &BookRegistry{
		books: make(map[int64]*book.BookDir),
		f:     f,
		lib:   lib,
	}
}

func (r *BookRegistry) FS() *fs.FS { return r.f }

// ResyncView rebuilds v's membership under the registry lock, so the rebuild
// is serialized against Add/Remove/commit and leaves no window for a lost or
// stale entry. v decides membership inside its own Add.
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

func (r *BookRegistry) RemoveView(v BookView) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i := slices.Index(r.views, v); i >= 0 {
		r.views = slices.Delete(r.views, i, i+1)
	}
}

// AddView registers v. Register every view before adding any book.
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

// commit brackets a snapshot swap with view removal and re-addition, so a view
// drops the book reading the old snapshot and re-files it reading the new one.
// Callers hold r.mu and must have persisted first, so a failed write never
// reaches the tree.
func (r *BookRegistry) commit(dir *book.BookDir, updated *library.Book) {
	for _, v := range r.views {
		v.Remove(dir)
	}
	dir.SetSnapshot(updated)
	for _, v := range r.views {
		v.Add(dir)
	}
}

func (r *BookRegistry) Add(book *library.Book) {
	r.mu.Lock()
	defer r.mu.Unlock()
	dir := r.dirLocked(book)
	for _, v := range r.views {
		v.Add(dir)
	}
}

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

// Edit persists edits and commits them, so a view rehomes the book when its
// grouping or name changed.
//
// lib.Edit can rewrite the whole epub, seconds of disk I/O, so it must not run
// under r.mu. The per-book editMu keeps concurrent edits of one book and their
// commits in order; r.mu is held only for the lookup and the commit bracket.
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
