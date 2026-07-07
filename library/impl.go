package library

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/config"
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
	// (Edit, WriteCover, Delete), so e.g. a cover rewrite cannot interleave
	// with an edit that is moving the book directory. Lazily created per id,
	// mirroring kepub.Cache's conversion locks.
	bookMuMu sync.Mutex
	bookMu   map[int64]*sync.Mutex

	// ingestMu serializes the entire ingest path (Exists → NextID → Layout →
	// Ingest → index Put) so two simultaneous uploads of the same new book
	// cannot both pass the Exists check before either lays the book down.
	ingestMu sync.Mutex
}

// lockBook returns the mutex serializing on-disk mutations of book id.
func (l *libraryImpl) lockBook(id int64) *sync.Mutex {
	l.bookMuMu.Lock()
	defer l.bookMuMu.Unlock()
	m, ok := l.bookMu[id]
	if !ok {
		m = &sync.Mutex{}
		l.bookMu[id] = m
	}
	return m
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

type exporterCloser interface{ close() error }

func (l *libraryImpl) Close() error {
	l.expMu.Lock()
	for _, e := range l.exporters {
		if c, ok := e.(exporterCloser); ok {
			c.close()
		}
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
	return &ingestHandle{file: f, path: f.Name(), ingestFn: l.ingestPath}, nil
}

// ingestPath parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) ingestPath(epubPath string) (*model.Book, error) {
	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

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
	b := model.NewBook(bib, meta, loc)

	if err := l.index.Put(b, func() error {
		return l.store.Ingest(epubPath, b.Location, &b.Meta)
	}); err != nil {
		// Index write failed after the store wrote; clean up so a retry starts fresh.
		_ = l.store.Delete(b.Location)
		return nil, err
	}

	log.Printf("ingest: book %d (%q) by %s", b.Meta.ID, b.Title, formatAuthors(bib.Authors))
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

// Reindex unconditionally rebuilds the index from the store (the source of
// truth). Books that can't be read are logged and skipped rather than failing
// the whole rebuild.
func (l *libraryImpl) Reindex() error {
	entries, err := l.store.Walk()
	if err != nil {
		return err
	}

	var (
		books []*model.Book
		maxID int64
	)
	for _, e := range entries {
		meta, err := l.store.ReadMeta(e)
		if err != nil {
			log.Printf("reindex: skip %s: read meta: %v", e.LibraryPath, err)
			continue
		}
		if meta.ID > maxID {
			maxID = meta.ID
		}

		book, err := epub.Parse(e.EpubPath)
		if err != nil {
			log.Printf("reindex: skip %s: parse epub: %v", e.LibraryPath, err)
			continue
		}

		books = append(books, model.NewBook(bibFromEpub(book), *meta, e))
	}

	if err := l.index.Rebuild(books, maxID); err != nil {
		return err
	}
	log.Printf("reindex: indexed %d of %d books", len(books), len(entries))
	return nil
}

// needsReindex reports whether the index requires a rebuild — true when the
// dirty flag is set or the schema version is stale.
func (l *libraryImpl) needsReindex() bool {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		log.Printf("reindex: could not check index state (%v), forcing rebuild", err)
		return true
	}
	return needs
}

// OpenEpub returns a handle to the epub content of book id. The caller must
// close it. The book's current location is resolved fresh, so the handle always
// tracks the live file even if a concurrent edit moved it.
func (l *libraryImpl) OpenEpub(id int64) (EpubReader, error) {
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
	mu := l.lockBook(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return nil, err
	}

	if v := e.Validate(b); v != nil {
		log.Printf("edit: book %d (%q): validation failed: %v", b.Meta.ID, b.Title, v)
		return nil, v
	}

	// Assemble the updated book from the meta edits; bib fields are derived
	// below from the epub re-parse.
	updated := applyMeta(b, e)

	if err := l.index.Put(updated, func() error {
		var reparsed *epub.Book

		if e.HasCoverEdit() {
			re, err := epub.WriteCover(b.EpubPath, b.CoverPath, *e.Cover)
			if err != nil {
				log.Printf("edit: book %d (%q): replace cover: %v", b.Meta.ID, b.Title, err)
				return err
			}
			reparsed = re
		}

		if e.HasBibEdits() {
			re, err := epub.WriteBib(b.EpubPath, e)
			if err != nil {
				log.Printf("edit: book %d (%q): rewrite epub: %v", b.Meta.ID, b.Title, err)
				return err
			}
			reparsed = re
		}

		if reparsed != nil {
			updated.Bib = bibFromEpub(reparsed)
		}

		newLoc := l.store.Layout(updated.Authors, updated.Title, updated.Meta.ID)
		if newLoc.LibraryPath != b.Location.LibraryPath || newLoc.EpubFilename != b.Location.EpubFilename {
			if err := l.store.Move(b.Location, newLoc); err != nil {
				log.Printf("edit: book %d (%q): move directory: %v", b.Meta.ID, b.Title, err)
				return err
			}
			updated.Location = newLoc
		}
		if err := l.store.WriteMeta(updated.Location, &updated.Meta); err != nil {
			log.Printf("edit: book %d (%q): write meta: %v", b.Meta.ID, b.Title, err)
			return err
		}
		return nil
	}); err != nil {
		return nil, err
	}

	return updated, nil
}

// applyMeta returns a copy of b with the meta edits in e applied and the
// modified time stamped. Fields left nil in e are untouched. Bib fields are not
// applied here — Edit derives them from the epub re-parse.
func applyMeta(b *model.Book, e model.Edits) *model.Book {
	cp := *b
	if e.Status != nil {
		cp.Meta.Status = *e.Status
	}
	if e.Rating != nil {
		cp.Meta.Rating = *e.Rating
	}
	if e.Tags != nil {
		cp.Meta.Tags = *e.Tags
	}
	cp.Meta.DateModified = time.Now()
	return &cp
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
		EpubSize:    src.EpubSize,
	}
}

// Delete removes the book with the given id from the store and the index,
// resolving its current location under the per-book lock.
func (l *libraryImpl) Delete(id int64) error {
	mu := l.lockBook(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return err
	}
	// Store is authoritative; a ghost index row is cleaned up by reindex.
	err = l.index.Delete(id, func() error { return l.store.Delete(b.Location) })
	if err != nil {
		log.Printf("delete: book %d (%q): %v", id, b.Title, err)
	} else {
		log.Printf("delete: book %d (%q): ok", id, b.Title)
	}
	return err
}

func formatAuthors(authors []model.Author) string {
	var names []string
	for _, a := range authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return model.UnknownAuthor
	}
	return strings.Join(names, ", ")
}
