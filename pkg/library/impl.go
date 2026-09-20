package library

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

// Library is the backend facade: the store, the index, and the locks that keep
// them agreeing. Construct it with Open.
//
// Concurrency contract: methods are safe for concurrent use. Search returns
// *Book values that are immutable snapshots — the library never mutates a
// Book after returning it. Every other operation addresses a book by id and
// resolves its current state fresh, so callers never pass stale snapshots back
// in: Content opens the book's live on-disk file, and mutations (Edit, Delete)
// run as an atomic read-modify-write per book under a per-book lock, so callers
// cannot revert other callers' changes.
type Library struct {
	store     *store.Store
	index     *index.Index
	inboxTemp string

	// Exporters that own resources (the kepub cache and its warmer), collected
	// as they are handed out; releasing them is the library's job (see Exporter).
	closers  []io.Closer
	closerMu sync.Mutex
	// Dedup of exporters by config is not implemented. If needed in the
	// future, hash/comparable-key the ReaderConfig fields and store in a map.

	// bookMu serializes the operations that mutate one book's on-disk state
	// (Edit, Delete), so e.g. a cover rewrite cannot interleave with an edit
	// that is moving the book directory.
	bookMu syncutil.KeyedMutex

	// ingestMu serializes the entire ingest path (Exists → NextID → Layout →
	// Ingest → index Put) so two simultaneous uploads of the same new book
	// cannot both pass the Exists check before either lays the book down.
	ingestMu sync.Mutex

	// mutateMu excludes a runtime Reindex from every other mutation. Reindex
	// moves book directories to their canonical paths and rebuilds the index
	// wholesale, neither of which addresses a single book, so the per-book lock
	// cannot cover it: a concurrent Edit renames the directory the move is
	// reading. Held for reading by the per-book mutations and by ingest, for
	// writing by Reindex. Always taken before bookMu and ingestMu.
	mutateMu sync.RWMutex
}

func (l *Library) Close() error {
	l.closerMu.Lock()
	for _, c := range l.closers {
		if err := c.Close(); err != nil {
			slog.Error("close: exporter failed", "error", err)
		}
	}
	l.closerMu.Unlock()
	return l.index.Close()
}

func (l *Library) Search(q Query) ([]*Book, error) {
	books, err := l.index.Search(q)
	if err != nil {
		return nil, err
	}
	result := make([]*Book, len(books))
	for i, b := range books {
		result[i] = book.NewImmutableBook(b)
	}
	return result, nil
}

// Stats returns aggregate library statistics.
func (l *Library) Stats() (*Stats, error) {
	return l.index.Stats()
}

// Get returns the book with the given id, or an error wrapping ErrBookNotFound
// when the index does not hold it. The returned Book is an immutable snapshot;
// see the concurrency contract on Library.
func (l *Library) Get(id int64) (*Book, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return book.NewImmutableBook(b), nil
}

// get returns the current state of book id from the index, hydrated with its
// absolute epub path. Mutations fetch their base through it under the per-book
// lock, so they always operate on the book's authoritative current state.
func (l *Library) get(id int64) (*book.Book, error) {
	b, err := l.index.Get(id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("book %d: %w", id, ErrBookNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("book %d: %w", id, err)
	}
	return b, nil
}

// Content returns an open handle to the book's epub content. The caller must
// close it.
func (l *Library) Content(id int64) (EpubReader, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.OpenReader(l.store.AbsPath(b.EpubPath), b.CoverPath)
}

// Edit applies edits to the book with the given id, persists everything, and
// returns the updated book. The edit base is the book's current state, fetched
// under the per-book lock — an atomic read-modify-write, so concurrent callers
// cannot revert each other's changes by editing from stale snapshots. If the
// title or authors change, the book directory is moved.
func (l *Library) Edit(id int64, e Edits) (*Book, error) {
	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

	mu := l.bookMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return nil, err
	}

	// Every edit is validated here at the facade — the single enforcement
	// point — so meta-only edits (which skip the epub rewrite) can't slip
	// through unchecked.
	e = e.Normalized()
	if v := book.Validate(e, b); v != nil {
		return nil, v
	}

	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return nil, err
	}

	meta := applyMeta(b.Meta, e)
	bib, err := epub.Rewrite(l.store.AbsPath(b.EpubPath), b, e)
	if err != nil {
		op.Cancel()
		slog.Error("edit: rewrite failed", "book_id", b.Meta.ID, "title", b.Title, "error", err)
		return nil, err
	}

	location := l.store.Layout(bib.Authors, bib.Title, meta.ID)
	mt, err := l.store.Update(b.Location, location, &meta)
	if err != nil {
		slog.Error("edit: update failed", "book_id", b.Meta.ID, "title", b.Title, "error", err)
		return nil, err
	}

	updated := bookFromBib(bib, meta, location, mt)
	if err := op.Put(updated, mt); err != nil {
		return nil, err
	}
	return book.NewImmutableBook(updated), nil
}

// Delete removes the book with the given id from the store and the index,
// resolving its current location under the per-book lock.
func (l *Library) Delete(id int64) error {
	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

	mu := l.bookMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return err
	}
	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return err
	}
	// Store is authoritative; a ghost index row is cleaned up by reindex.
	err = l.store.Delete(b.Location)
	if err != nil {
		slog.Error("delete: store delete failed", "book_id", id, "title", b.Title, "error", err)
		return err
	}
	if err := op.Delete(id); err != nil {
		slog.Error("delete: index delete failed", "book_id", id, "title", b.Title, "error", err)
		return err
	}
	slog.Info("delete: book removed", "book_id", id, "title", b.Title)
	return nil
}

// applyMeta returns a copy of m with the meta edits in e applied and the
// modified time stamped. Fields left nil in e are untouched. Bib fields are not
// applied here — Edit derives them from the epub re-parse.
//
// The result shares nothing with its arguments. Taking m by value covers the
// scalars, but Tags is a slice and would otherwise alias whichever of the two
// it came from — the caller's Meta when the edit is absent, the caller's Edits
// when it is present. Both are live objects the caller still holds, and the
// result travels on to the sidecar write and the index, so the copy is made
// here once rather than left as a caveat every caller has to know about.
func applyMeta(m book.Meta, e Edits) book.Meta {
	if e.Status != nil {
		m.Status = *e.Status
	}
	if e.Rating != nil {
		m.Rating = *e.Rating
	}
	if e.Tags != nil {
		m.Tags = *e.Tags
	}
	m.Tags = slices.Clone(m.Tags)
	m.DateModified = time.Now()
	return m
}

// bookFromBib creates a complete Book from a bib, meta, location, and observation.
// The Book is fully populated when returned, with EpubSize set from the observation,
// so callers don't need to set it separately.
func bookFromBib(bib book.Bib, meta book.Meta, loc book.Location, obs drift.PathInfo) *book.Book {
	b := book.NewBook(bib, meta, loc)
	b.EpubSize = obs.Size
	return b
}
