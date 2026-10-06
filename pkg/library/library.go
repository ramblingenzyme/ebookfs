package library

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/util/syncutil"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

// Library is the backend facade: the store, the index, and the locks that keep
// them agreeing. Construct it with Open.
//
// Its methods are safe for concurrent use. A returned *Book is a snapshot the
// library never mutates. Every other call names a book by id and reads its
// current state, so a caller never passes a stale snapshot back. Edit and
// Delete are an atomic read-modify-write under a per-book lock, so callers
// cannot revert each other.
type Library struct {
	store     *store.Store
	index     *index.Index
	inboxTemp string

	// Exporters that own resources, such as the kepub cache, for Close.
	//
	// ponytail: two readers with identical config each get their own exporter.
	// Key a map on the ReaderConfig fields only if a deployment ever has enough
	// readers for the duplication to cost anything.
	closers  []io.Closer
	closerMu sync.Mutex

	// bookMu serializes Edit and Delete on one book, so a cover rewrite cannot
	// interleave with an edit moving the book directory.
	bookMu syncutil.KeyedMutex

	// ingestMu serializes ingest from the duplicate check to the index write,
	// so two uploads of one new book cannot both pass the check.
	ingestMu sync.Mutex

	// mutateMu excludes Reindex from every other mutation, since it moves
	// directories that no per-book lock covers. Mutations and ingest hold it
	// for reading, Reindex for writing. Always taken before bookMu and
	// ingestMu.
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

func (l *Library) Stats() (*Stats, error) {
	return l.index.Stats()
}

// Authors returns each author with a book count, ordered by sort name.
func (l *Library) Authors() ([]Facet, error) { return l.index.ListAuthors() }

// Series returns each series with a book count, ordered by name.
func (l *Library) Series() ([]Facet, error) { return l.index.ListSeries() }

// Tags returns each tag with a book count, ordered by name.
func (l *Library) Tags() ([]Facet, error) { return l.index.ListTags() }

// Get wraps ErrBookNotFound when the index does not hold the book.
func (l *Library) Get(id int64) (*Book, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return book.NewImmutableBook(b), nil
}

// get reads the book's current state from the index. Edit and Delete call it
// under the per-book lock, so they build on the state they then write.
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

func (l *Library) Content(id int64) (EpubReader, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.OpenReader(l.store.AbsPath(b.EpubPath), b.CoverPath)
}

// Edit applies e and returns the updated book. A title or author change moves
// the book directory.
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

	// Validated here, the one enforcement point, since a meta-only edit never
	// reaches the epub rewrite.
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
	// Store first: a leftover index row is healed by reindex.
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

// applyMeta stamps the modified time and leaves a nil field alone. The result
// shares nothing with its arguments: m is copied by value, but Tags is cloned,
// since the caller still holds both Meta and Edits.
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

func bookFromBib(bib book.Bib, meta book.Meta, loc book.Location, obs drift.PathInfo) *book.Book {
	b := book.NewBook(bib, meta, loc)
	b.EpubSize = obs.Size
	return b
}

// WithSidecars calls fn with an os.Root scoped to the book's .sidecar/
// directory. The per-book lock is held for the duration — Edit and Delete
// block until fn returns. The Root is closed after fn returns.
//
// The caller should not escape file handles from the callback: on Linux the
// Root tracks the directory via fd, so escaped handles remain valid after a
// rename, but the lock is released and the handle may observe a stale state.
func (l *Library) WithSidecars(id int64, fn func(*os.Root) error) error {
	l.mutateMu.RLock()
	defer l.mutateMu.RUnlock()

	mu := l.bookMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return err
	}
	root, err := l.store.OpenSidecars(b.Location)
	if err != nil {
		return err
	}
	defer root.Close()
	return fn(root)
}

// ReadSidecar reads a sidecar file. The per-book lock is held for the read.
func (l *Library) ReadSidecar(id int64, name string) ([]byte, error) {
	var data []byte
	err := l.WithSidecars(id, func(root *os.Root) error {
		var err error
		data, err = root.ReadFile(name)
		return err
	})
	return data, err
}

// WriteSidecar writes a sidecar file atomically (temp file + rename). The
// per-book lock is held for the write.
func (l *Library) WriteSidecar(id int64, name string, data []byte) error {
	return l.WithSidecars(id, func(root *os.Root) error {
		tmpName := "." + name + ".tmp"
		if err := root.WriteFile(tmpName, data, 0644); err != nil {
			return err
		}
		return root.Rename(tmpName, name)
	})
}
