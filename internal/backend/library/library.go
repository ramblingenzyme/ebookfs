package library

import (
	"fmt"
	"io"
	"io/fs"
	"log"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/backend/epub"
	"github.com/ramblingenzyme/ebookfs/internal/backend/index"
	"github.com/ramblingenzyme/ebookfs/internal/backend/store"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// EpubReader is a handle to a book's epub content. It hides where the bytes
// live (currently a file on disk) from the 9P layer, which needs random reads,
// the file size (via Stat), and a close.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	Stat() (fs.FileInfo, error)
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

	b := bookFromParts(book, &model.Meta{})
	if l.store.Exists(b.Authors, b.Title) {
		return nil, fmt.Errorf("book already in library: %q", b.Title)
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	b.Meta = model.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
		Status:       "unread",
		Rating:       0,
		Tags:         []string{},
	}
	b.Location = store.Layout(b.Authors, b.Title, id)

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
	return l.index.ListAll()
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

		book, err := epub.Parse(l.store.AbsPath(e.LibraryPath, e.EpubFilename))
		if err != nil {
			log.Printf("reindex: skip %s: parse epub: %v", e.LibraryPath, err)
			continue
		}

		b := bookFromParts(book, meta)
		b.Location = e
		books = append(books, b)
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
	return epub.ExtractCover(l.store.AbsPath(b.LibraryPath, b.EpubFilename), b.CoverPath)
}

func (l *Library) ReadMeta(b *model.Book) (*model.Meta, error) {
	return l.store.ReadMeta(b.Location)
}

func (l *Library) WriteMeta(b *model.Book) error {
	// Sidecar is written first because it is the source of truth. If the index
	// update fails, the sidecar still holds the correct state and reindex recovers.
	if err := l.store.WriteMeta(b.Location, &b.Meta); err != nil {
		return err
	}

	return l.index.Put(b)
}

// TODO: handle multiple authors like Calibre — take newAuthors []model.Author,
// file under the primary (first) author, and preserve the full ordered list.
// Today Move collapses the book to a single author, dropping any co-authors from
// the index on Put.
func (l *Library) Move(b *model.Book, newAuthor model.Author, newTitle string) (*model.Book, error) {
	to := store.Layout([]model.Author{newAuthor}, newTitle, b.Meta.ID)
	if err := l.store.Move(b.Location, to); err != nil {
		return nil, err
	}

	updated := *b
	updated.Title = newTitle
	updated.Authors = []model.Author{newAuthor}
	updated.Location = to
	updated.Meta.DateModified = time.Now()

	if err := l.index.Put(&updated); err != nil {
		// File is already moved; the index is stale until reindex recovers it.
		return nil, err
	}

	return &updated, nil
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

// bookFromParts assembles a model.Book from the bibliographic data parsed out of
// the epub plus its fresh sidecar. The caller fills in b.Location once the
// canonical layout has been computed (see store.Layout).
func bookFromParts(src *epub.Book, meta *model.Meta) *model.Book {
	authors := make([]model.Author, len(src.Authors))
	for i, a := range src.Authors {
		authors[i] = model.Author{Name: a.Name, SortName: a.SortAs}
	}

	var series *model.SeriesRef
	if src.Series != "" {
		series = &model.SeriesRef{Name: src.Series, Index: src.SeriesIndex}
	}

	identifiers := make(map[string]string, len(src.Identifiers))
	for _, ident := range src.Identifiers {
		identifiers[ident.ID] = ident.Value
	}

	pubdate := src.PubDate

	return &model.Book{
		Meta: *meta,
		Bib: model.Bib{
			Title:       src.Title,
			SortTitle:   src.SortTitle,
			Authors:     authors,
			Series:      series,
			Language:    src.Language,
			Description: src.Description,
			Pubdate:     pubdate,
			Identifiers: identifiers,
			CoverPath:   src.CoverPath,
		},
	}
}
