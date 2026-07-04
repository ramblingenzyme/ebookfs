package library

import (
	"fmt"
	"log"
	"os"
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
	return e, nil
}

func (l *libraryImpl) CreateIngest() (*IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}
	return NewIngestHandle(f, f.Name(), l.ingestPath), nil
}

// ingestPath parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) ingestPath(epubPath string) (*model.Book, error) {
	book, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	bib := bibFromEpub(book)
	if bib.Title == "" {
		return nil, fmt.Errorf("epub has no title")
	}
	if len(bib.Authors) == 0 {
		bib.Authors = []model.Author{{Name: "Unknown", SortName: "Unknown"}}
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

	return b, nil
}

func (l *libraryImpl) ListAll() ([]*model.Book, error) {
	books, err := l.index.ListAll()
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

// Reindex rebuilds the index from the store (the source of truth). Skips
// entirely (O(1)) when the dirty flag is clear. Books that can't be read are
// logged and skipped rather than failing the whole rebuild.
func (l *libraryImpl) Reindex() error {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		log.Printf("reindex: could not check index state (%v), forcing rebuild", err)
		needs = true
	}
	if !needs {
		log.Printf("reindex: index is clean, skipping")
		return nil
	}

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

// OpenEpub returns a handle to b's epub content. The caller must close it.
func (l *libraryImpl) OpenEpub(b *model.Book) (EpubReader, error) {
	return l.store.OpenEpub(b.Location)
}

// ExtractCover returns the cover image bytes from b's epub.
func (l *libraryImpl) ExtractCover(b *model.Book) ([]byte, error) {
	return epub.ExtractCover(b.EpubPath, b.CoverPath)
}

// ExtractOPF returns the raw OPF XML bytes from b's epub.
func (l *libraryImpl) ExtractOPF(b *model.Book) ([]byte, error) {
	return epub.ExtractOPF(b.EpubPath)
}

// Edit applies edits to a book, persists everything, and returns the updated
// book. If the title or authors change, the book directory is moved.
func (l *libraryImpl) Edit(b *model.Book, e model.Edits) (*model.Book, error) {
	if v := e.Validate(b); v != nil {
		return nil, v
	}

	updated := b.Edit(e)

	if err := l.index.Put(updated, func() error {
		if e.HasBibEdits() {
			re, err := epub.WriteBib(b.EpubPath, e)
			if err != nil {
				return err
			}
			updated.Bib = bibFromEpub(re)
		}
		newLoc := l.store.Layout(updated.Authors, updated.Title, updated.Meta.ID)
		if newLoc != b.Location {
			if err := l.store.Move(b.Location, newLoc); err != nil {
				return err
			}
			updated.Location = newLoc
		}
		return l.store.WriteMeta(updated.Location, &updated.Meta)
	}); err != nil {
		return nil, err
	}

	return updated, nil
}

// WriteCover replaces the cover image in b's epub with img.
func (l *libraryImpl) WriteCover(b *model.Book, img []byte) error {
	_, err := epub.WriteCover(b.EpubPath, b.CoverPath, img)
	return err
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
	}
}

func (l *libraryImpl) Delete(b *model.Book) error {
	// Store is authoritative; a ghost index row is cleaned up by reindex.
	return l.index.Delete(b.Meta.ID, func() error { return l.store.Delete(b.Location) })
}
