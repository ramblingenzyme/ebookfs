package library

import (
	"io"
	"io/fs"
	"log"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
	"github.com/ramblingenzyme/ebookfs/internal/index"
	"github.com/ramblingenzyme/ebookfs/internal/model"
	"github.com/ramblingenzyme/ebookfs/internal/store"
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
func (l *Library) Ingest(tmpPath string) (*model.Book, error) {
	book, err := epub.Parse(tmpPath)
	if err != nil {
		return nil, err
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	meta := &model.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
		Status:       "unread",
		Rating:       0,
		Tags:         []string{},
	}

	b := bookFromParts(book, meta)
	b.LibraryPath = store.CanonicalPath(b.Authors, b.Title, id)
	b.EpubFilename = store.EpubFilename(b.Authors, b.Title)

	if err := l.store.Ingest(b, tmpPath); err != nil {
		return nil, err
	}

	if err := l.index.Put(b); err != nil {
		// Store wrote successfully but the index did not; roll the store back so a
		// retry starts clean. The store is authoritative, so reindex would also recover.
		_ = l.store.Delete(b)
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
		meta, err := l.store.ReadMeta(e.LibraryPath)
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
		b.LibraryPath = e.LibraryPath
		b.EpubFilename = e.EpubFilename
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
	return l.store.OpenEpub(b)
}

func (l *Library) ReadMeta(b *model.Book) (*model.Meta, error) {
	return l.store.ReadMeta(b.LibraryPath)
}

func (l *Library) WriteMeta(b *model.Book) error {
	// Sidecar is written first because it is the source of truth. If the index
	// update fails, the sidecar still holds the correct state and reindex recovers.
	if err := l.store.WriteMeta(b.LibraryPath, &b.Meta); err != nil {
		return err
	}

	return l.index.Put(b)
}

func (l *Library) Move(b *model.Book, newAuthor model.Author, newTitle string) (*model.Book, error) {
	newLibraryPath, newEpubFilename, err := l.store.Move(b, newAuthor, newTitle)
	if err != nil {
		return nil, err
	}

	updated := *b
	updated.Title = newTitle
	updated.Authors = []model.Author{newAuthor}
	updated.LibraryPath = newLibraryPath
	updated.EpubFilename = newEpubFilename
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
	if err := l.store.Delete(b); err != nil {
		return err
	}

	return l.index.DeleteBook(b.Meta.ID)
}

// bookFromParts assembles a model.Book from the bibliographic data parsed out of
// the epub plus its fresh sidecar. The caller fills in LibraryPath/EpubFilename
// once the canonical path has been computed.
func bookFromParts(src *epub.Book, meta *model.Meta) *model.Book {
	authors := make([]model.Author, len(src.Authors))
	for i, a := range src.Authors {
		authors[i] = model.Author{Name: a.Name, SortName: a.SortAs}
	}

	var series *model.SeriesRef
	if src.Series != "" {
		series = &model.SeriesRef{Name: src.Series, Index: float64(src.SeriesIndex)}
	}

	identifiers := make(map[string]string, len(src.Identifiers))
	for _, ident := range src.Identifiers {
		identifiers[ident.ID] = ident.Value
	}

	pubdate := ""
	if !src.PubDate.IsZero() {
		pubdate = src.PubDate.Format(time.RFC3339)
	}

	return &model.Book{
		Meta:         *meta,
		Title:        src.Title,
		SortTitle:    src.SortTitle,
		Authors:      authors,
		Series:       series,
		Description:  src.Description,
		Pubdate:      pubdate,
		Identifiers:  identifiers,
		HasCover:     src.CoverPath != "",
	}
}
