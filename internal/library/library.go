package library

import (
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
	"github.com/ramblingenzyme/ebookfs/internal/index"
	"github.com/ramblingenzyme/ebookfs/internal/model"
	"github.com/ramblingenzyme/ebookfs/internal/store"
)

// Library coordinates filesystem and index operations on the book collection.
// It is the primary API for the 9P layer; store and index are implementation details.
type Library struct {
	store *store.Store
	index *index.Index
}

func New(s *store.Store, idx *index.Index) *Library {
	return &Library{store: s, index: idx}
}

func (l *Library) Ingest(book *epub.Book, tmpPath string) (*model.Book, error) {
	tx, err := l.index.Begin()
	if err != nil {
		return nil, err
	}

	id, err := l.index.AllocateID(tx)
	if err != nil {
		tx.Rollback()
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

	libraryPath, epubFilename, err := l.store.Ingest(id, book, tmpPath, meta)
	if err != nil {
		tx.Rollback()
		return nil, err
	}

	b := bookFromParts(book, libraryPath, epubFilename, meta)

	if err = l.index.InsertBook(tx, b); err != nil {
		tx.Rollback()
		_ = l.store.Delete(b)
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		_ = l.store.Delete(b)
		return nil, err
	}

	return b, nil
}

func (l *Library) ListAll() ([]*model.Book, error) {
	return l.index.ListAll()
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

	tx, err := l.index.Begin()
	if err != nil {
		return err
	}

	if err = l.index.UpdateMeta(tx, b); err != nil {
		tx.Rollback()
		return err
	}

	return tx.Commit()
}

func (l *Library) Move(b *model.Book, newAuthor epub.Author, newTitle string) (*model.Book, error) {
	newLibraryPath, newEpubFilename, err := l.store.Move(b, newAuthor, newTitle)
	if err != nil {
		return nil, err
	}

	updated := *b
	updated.Title = newTitle
	updated.Authors = []model.Author{{Name: newAuthor.Name, SortName: newAuthor.SortAs}}
	updated.LibraryPath = newLibraryPath
	updated.EpubFilename = newEpubFilename

	tx, err := l.index.Begin()
	if err != nil {
		return nil, err
	}

	if err = l.index.MoveBook(tx, &updated); err != nil {
		tx.Rollback()
		// file is already moved; index is stale until reindex
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &updated, nil
}

func (l *Library) Delete(b *model.Book) error {
	tx, err := l.index.Begin()
	if err != nil {
		return err
	}

	if err = l.index.DeleteBook(tx, b.Meta.ID); err != nil {
		tx.Rollback()
		return err
	}

	if err = l.store.Delete(b); err != nil {
		tx.Rollback()
		return err
	}

	// If Commit fails after store.Delete succeeded, the directory is gone but the
	// transaction rolls back, leaving a ghost index row. Reindex recovers: it walks
	// the filesystem and the missing directory causes the stale row to be removed.
	return tx.Commit()
}

// bookFromParts assembles a model.Book from the parts available at ingest time.
func bookFromParts(src *epub.Book, libraryPath, epubFilename string, meta *model.Meta) *model.Book {
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
		LibraryPath:  libraryPath,
		EpubFilename: epubFilename,
		HasCover:     src.CoverPath != "",
	}
}
