package library

import (
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type libraryImpl struct {
	store     *store.Store
	index     *index.Index
	inboxTemp string
	exporters []Exporter
	expMu     sync.Mutex
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

// get returns the current state of book id from the index, hydrated with its
// absolute epub path. Mutations fetch their base through it under the per-book
// lock, so they always operate on the book's authoritative current state.
func (l *libraryImpl) get(id int64) (*model.Book, error) {
	b, err := l.index.Get(id)
	if err != nil {
		return nil, fmt.Errorf("no book with id %d: %w", id, err)
	}
	b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	return b, nil
}

func (l *libraryImpl) Close() error {
	l.expMu.Lock()
	for _, e := range l.exporters {
		_ = e.Close()
	}
	l.expMu.Unlock()
	return l.index.Close()
}

func (l *libraryImpl) Exporter(cfg config.ReaderConfig) (Exporter, error) {
	e, err := newExporter(cfg, l)
	if err != nil {
		return nil, err
	}
	l.expMu.Lock()
	l.exporters = append(l.exporters, e)
	l.expMu.Unlock()
	kind := "epub"
	if cfg.Convert {
		kind = "kepub"
	}
	log.Printf("export: %s for statuses %v", kind, cfg.Statuses)
	return e, nil
}

func (l *libraryImpl) CreateIngest() (IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}
	return &ingestHandle{file: f, ingestFn: l.ingestPath}, nil
}

// ingestPath parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) ingestPath(epubPath string) (*model.Book, error) {
	// Parse before taking ingestMu: it touches only this upload's staged temp
	// file, so bulk uploads overlap their parsing instead of serializing on it.
	book, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	bib := bibFromEpub(book)
	if bib.Title == "" {
		return nil, fmt.Errorf("epub has no title")
	}
	if len(bib.Authors) == 0 {
		bib.Authors = []model.Author{{Name: model.UnknownAuthor, SortName: model.UnknownAuthor}}
	}

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

	if l.store.Exists(bib.Authors, bib.Title) {
		return nil, fmt.Errorf("book already in library: %q", bib.Title)
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	meta := model.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
	}
	loc := l.store.Layout(bib.Authors, bib.Title, id)

	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return nil, err
	}
	if err := l.store.Ingest(epubPath, loc, &meta); err != nil {
		// Ingest failed; the pending row stays (forcing a healing reindex) and
		// we clean up the store so a retry starts fresh.
		_ = l.store.Delete(loc)
		return nil, err
	}
	mt, err := l.store.Stat(loc)
	if err != nil {
		_ = l.store.Delete(loc)
		return nil, err
	}
	b := bookFromBib(bib, meta, loc, mt)
	if err := op.Put(b, mt); err != nil {
		_ = l.store.Delete(b.Location)
		return nil, err
	}

	log.Printf("ingest: book %d (%q) by %s", b.Meta.ID, b.Title, model.JoinAuthors(bib.Authors, ", "))
	return b, nil
}

func (l *libraryImpl) Query(f model.Filter) ([]*model.Book, error) {
	books, err := l.index.Query(f)
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

func (l *libraryImpl) Search(q model.Query) ([]*model.Book, error) {
	books, err := l.index.Search(q)
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

// Stats returns aggregate library statistics.
func (l *libraryImpl) Stats() (*model.Stats, error) {
	return l.index.Stats()
}

// OpenEpub returns a handle to the epub content of book id. The caller must
// close it. The book's current location is resolved fresh, so the handle always
// tracks the live file even if a concurrent edit moved it.
func (l *libraryImpl) OpenEpub(id int64) (model.EpubReader, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	f, err := l.store.OpenEpub(b.Location)
	if err != nil {
		log.Printf("open: book %d (%q): %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	return f, nil
}

// ExtractCover returns the cover image bytes from book id's epub.
func (l *libraryImpl) ExtractCover(id int64) ([]byte, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.ExtractCover(b.EpubPath, b.CoverPath)
}

// ExtractOPF returns the raw OPF XML bytes from book id's epub.
func (l *libraryImpl) ExtractOPF(id int64) ([]byte, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.ExtractOPF(b.EpubPath)
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

	c, err := epub.Prepare(b, e)
	if err != nil {
		log.Printf("edit: book %d (%q): prepare rewrite: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	if err := op.MarkPending(); err != nil {
		c.Discard()
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		log.Printf("edit: book %d (%q): commit rewrite: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}

	bib := b.Bib
	if book := c.Book(); book != nil {
		bib = bibFromEpub(book)
	}

	meta := applyMeta(b.Meta, e)
	location := l.store.Layout(bib.Authors, bib.Title, meta.ID)
	if location.LibraryPath != b.Location.LibraryPath || location.EpubFilename != b.Location.EpubFilename {
		if err := l.store.Move(b.Location, location); err != nil {
			log.Printf("edit: book %d (%q): move directory: %v", b.Meta.ID, b.Title, err)
			return nil, err
		}
	}
	if err := l.store.WriteMeta(location, &meta); err != nil {
		log.Printf("edit: book %d (%q): write meta: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}

	mt, err := l.store.Stat(location)
	if err != nil {
		log.Printf("edit: book %d (%q): stat: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}

	updated := bookFromBib(bib, meta, location, mt)
	if err := op.Put(updated, mt); err != nil {
		return nil, err
	}
	return updated, nil
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

// bibFromEpub converts a parsed epub.Book into a model.Bib.
func bibFromEpub(src *epub.Book) model.Bib {
	var series *model.SeriesRef
	if src.Series != "" {
		series = &model.SeriesRef{Name: src.Series, Index: src.SeriesIndex}
	}

	identifiers := make(map[string]string, len(src.Identifiers))
	for _, ident := range src.Identifiers {
		identifiers[ident.ID] = ident.Value
	}

	return model.Bib{
		Title:       src.Title,
		SortTitle:   src.SortTitle,
		Authors:     src.Authors,
		Series:      series,
		Language:    src.Language,
		Pubdate:     src.PubDate,
		Description: src.Description,
		Identifiers: identifiers,
		CoverPath:   src.CoverPath,
		OpfSize:     src.OpfSize,
		CoverSize:   src.CoverSize,
	}
}

// bookFromBib creates a complete Book from a bib, meta, location, and observation.
// The Book is fully populated when returned, with EpubSize set from the observation,
// so callers don't need to set it separately.
func bookFromBib(bib model.Bib, meta model.Meta, loc model.Location, obs drift.PathInfo) *model.Book {
	b := model.NewBook(bib, meta, loc)
	b.EpubSize = obs.Size
	return b
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
		log.Printf("delete: book %d (%q): %v", id, b.Title, err)
		return err
	}
	if err := op.Delete(id); err != nil {
		log.Printf("delete: book %d (%q): %v", id, b.Title, err)
		return err
	}
	log.Printf("delete: book %d (%q): ok", id, b.Title)
	return nil
}
