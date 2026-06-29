package library

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/backend/epub"
	"github.com/ramblingenzyme/ebookfs/internal/backend/index"
	"github.com/ramblingenzyme/ebookfs/internal/backend/store"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// EpubReader is a handle to a book's epub content. It hides where the bytes
// live (currently a file on disk) from the 9P layer, which needs random reads
// and a close.
type EpubReader interface {
	io.ReaderAt
	io.Closer
}

// Library coordinates filesystem and index operations on the book collection.
// It is the primary API for the 9P layer; store and index are implementation details.
type Library struct {
	store *store.Store
	index *index.Index
}

func New(s *store.Store, idx *index.Index) *Library {
	return &Library{store: s, index: idx}
}

// Ingest parses the staged epub at tmpPath, lays it down in the store under its
// canonical path, and records it in the index. epub stays an implementation
// detail of this method; nothing above the library sees epub types.
func (l *Library) Ingest(epubPath string) (*model.Book, error) {
	book, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	bib := bibFromEpub(book)
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

	if err := l.store.Ingest(epubPath, b.Location, &b.Meta); err != nil {
		return nil, err
	}

	if err := l.index.Put(b); err != nil {
		// Store wrote successfully but the index did not; roll the store back so a
		// retry starts clean. The store is authoritative, so reindex would also recover.
		_ = l.store.Delete(b.Location)
		return nil, err
	}

	return b, nil
}

func (l *Library) ListAll() ([]*model.Book, error) {
	books, err := l.index.ListAll()
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

// Reindex rebuilds the index from the store, which is the source of truth. The
// store is authoritative, so this recovers from a missing, stale, or partially
// written index — it is the backstop the write paths' compensation relies on.
// A book whose meta or epub can't be read is logged and skipped rather than
// failing the whole rebuild; its id still counts toward the sequence high-water
// mark so future ingests can't reuse it.
func (l *Library) Reindex() error {
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

// OpenEpub returns a handle to b's epub content. The caller closes it. The 9P
// layer reads through this rather than touching the filesystem directly.
func (l *Library) OpenEpub(b *model.Book) (EpubReader, error) {
	return l.store.OpenEpub(b.Location)
}

// ExtractCover returns the cover image bytes from b's epub.
func (l *Library) ExtractCover(b *model.Book) ([]byte, error) {
	return epub.ExtractCover(b.EpubPath, b.CoverPath)
}

// Edit applies edits to a book, persists everything, and returns the updated
// book. Bib (OPF) edits trigger an epub rewrite and re-parse. Meta edits are
// applied in-memory. If the title or authors change the canonical location, the
// book directory is moved. WriteCover handles binary cover replacement.
func (l *Library) Edit(b *model.Book, e model.Edits) (*model.Book, error) {
	if v := e.Validate(b); v != nil {
		return nil, v
	}

	updated := b.Edit(e)

	if e.HasBibEdits() {
		re, err := epub.WriteBib(b.EpubPath, e)
		if err != nil {
			return nil, err
		}
		updated.Bib = bibFromEpub(re)
	}

	newLoc := l.store.Layout(updated.Authors, updated.Title, updated.Meta.ID)
	if newLoc != b.Location {
		if err := l.store.Move(b.Location, newLoc); err != nil {
			return nil, err
		}
		updated.Location = newLoc
	}

	if err := l.store.WriteMeta(updated.Location, &updated.Meta); err != nil {
		return nil, err
	}
	if err := l.index.Put(updated); err != nil {
		return nil, err
	}

	return updated, nil
}

// WriteCover replaces the cover image in b's epub with img, validates the image
// format matches the existing cover entry, and rewrites the epub atomically.
func (l *Library) WriteCover(b *model.Book, img []byte) error {
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

func (l *Library) Delete(b *model.Book) error {
	// Store is authoritative, so remove it first. If the index delete then fails,
	// the directory is gone but a ghost row remains; reindex walks the filesystem
	// and drops the stale row.
	if err := l.store.Delete(b.Location); err != nil {
		return err
	}

	return l.index.Delete(b.Meta.ID)
}


