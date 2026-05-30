package index

import (
	"database/sql"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// InsertBook inserts b into the index within tx.
func (idx *Index) InsertBook(tx *sql.Tx, b *model.Book) error {
	panic("not yet implemented")
}

// UpdateBook replaces all index data for b.ID within tx. Used when the epub's
// internal OPF is rewritten and bibliographic fields change.
func (idx *Index) UpdateBook(tx *sql.Tx, b *model.Book) error {
	panic("not yet implemented")
}

// MoveBook updates the path and author/title fields for b.ID within tx.
// b should reflect the post-move state.
func (idx *Index) MoveBook(tx *sql.Tx, b *model.Book) error {
	panic("not yet implemented")
}

// UpdateMeta updates the sidecar fields (status, rating, tags, date_modified)
// for b.ID within tx.
func (idx *Index) UpdateMeta(tx *sql.Tx, b *model.Book) error {
	panic("not yet implemented")
}

// DeleteBook removes all index rows for id within tx.
func (idx *Index) DeleteBook(tx *sql.Tx, id int64) error {
	panic("not yet implemented")
}

// GetBook returns the full index record for id.
func (idx *Index) GetBook(id int64) (*model.Book, error) {
	panic("not yet implemented")
}

// ListAuthors returns all authors in the index, ordered by sort_name.
func (idx *Index) ListAuthors() ([]*model.Author, error) {
	panic("not yet implemented")
}

// ListByAuthor returns all books by authorID, ordered by sort_title.
func (idx *Index) ListByAuthor(authorID int64) ([]*model.Book, error) {
	panic("not yet implemented")
}

// ListByTag returns all books with the given tag, ordered by sort_title.
func (idx *Index) ListByTag(tag string) ([]*model.Book, error) {
	panic("not yet implemented")
}

// ListByStatus returns all books with the given status, ordered by date_added desc.
func (idx *Index) ListByStatus(status string) ([]*model.Book, error) {
	panic("not yet implemented")
}

// ListBySeries returns all books in seriesID, ordered by series_index.
func (idx *Index) ListBySeries(seriesID int64) ([]*model.Book, error) {
	panic("not yet implemented")
}

// Recent returns the n most recently added books.
func (idx *Index) Recent(n int) ([]*model.Book, error) {
	panic("not yet implemented")
}

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*model.Stats, error) {
	panic("not yet implemented")
}
