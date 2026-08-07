package library

import (
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type libraryImpl struct {
	store           *store.Store
	index           *index.Index
	inboxTemp       string
	defaultExporter Exporter
	exporters       []Exporter
	expMu           sync.Mutex
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
}

func (l *libraryImpl) Close() error {
	l.expMu.Lock()
	for _, e := range l.exporters {
		if err := e.Close(); err != nil {
			slog.Error("close: exporter failed", "error", err)
		}
	}
	l.expMu.Unlock()
	return l.index.Close()
}

func (l *libraryImpl) Query(f model.Filter) ([]*model.Book, error) {
	books, err := l.index.Query(f)
	if err != nil {
		return nil, err
	}
	return books, nil
}

func (l *libraryImpl) Search(q model.Query) ([]*model.Book, error) {
	books, err := l.index.Search(q)
	if err != nil {
		return nil, err
	}
	return books, nil
}

// Stats returns aggregate library statistics.
func (l *libraryImpl) Stats() (*model.Stats, error) {
	return l.index.Stats()
}

// get returns the current state of book id from the index, hydrated with its
// absolute epub path. Mutations fetch their base through it under the per-book
// lock, so they always operate on the book's authoritative current state.
func (l *libraryImpl) get(id int64) (*model.Book, error) {
	b, err := l.index.Get(id)
	if err != nil {
		return nil, fmt.Errorf("no book with id %d: %w", id, err)
	}
	return b, nil
}

// Content returns an open handle to the book's epub content. The caller must
// close it.
func (l *libraryImpl) Content(id int64) (model.EpubReader, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return l.defaultExporter.Open(b)
}

// Edit applies edits to the book with the given id, persists everything, and
// returns the updated book. The edit base is the book's current state, fetched
// under the per-book lock — an atomic read-modify-write, so concurrent callers
// cannot revert each other's changes by editing from stale snapshots. If the
// title or authors change, the book directory is moved.
func (l *libraryImpl) Edit(id int64, e model.Edits) (*model.Book, error) {
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
	if v := e.Validate(b); v != nil {
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

	if bib == nil {
		bib = &b.Bib
	}

	location := l.store.Layout(bib.Authors, bib.Title, meta.ID)
	mt, err := l.store.Update(b.Location, location, &meta)
	if err != nil {
		slog.Error("edit: update failed", "book_id", b.Meta.ID, "title", b.Title, "error", err)
		return nil, err
	}

	updated := bookFromBib(*bib, meta, location, mt)
	if err := op.Put(updated, mt); err != nil {
		return nil, err
	}
	return updated, nil
}

// Delete removes the book with the given id from the store and the index,
// resolving its current location under the per-book lock.
func (l *libraryImpl) Delete(id int64) error {
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
func applyMeta(m model.Meta, e model.Edits) model.Meta {
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
func bookFromBib(bib model.Bib, meta model.Meta, loc model.Location, obs drift.PathInfo) *model.Book {
	b := model.NewBook(bib, meta, loc)
	b.EpubSize = obs.Size
	return b
}
