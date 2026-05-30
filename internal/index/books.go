package index

import (
	"database/sql"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

func upsertAuthors(tx *sql.Tx, bookID int64, authors []model.Author) error {
	if _, err := tx.Exec(`DELETE FROM book_authors WHERE book_id=?`, bookID); err != nil {
		return err
	}
	for i, a := range authors {
		// Insert or update: only overwrite sort_name when we have a real value and the
		// stored one is empty (fills in missing file-as data without stomping corrections).
		if _, err := tx.Exec(
			`INSERT INTO authors (name, sort_name) VALUES (?, ?)
			 ON CONFLICT(name) DO UPDATE SET sort_name=excluded.sort_name
			 WHERE excluded.sort_name != '' AND authors.sort_name = ''`,
			a.Name, a.SortName,
		); err != nil {
			return err
		}
		var authorID int64
		if err := tx.QueryRow(`SELECT id FROM authors WHERE name=?`, a.Name).Scan(&authorID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO book_authors (book_id, author_id, position) VALUES (?, ?, ?)`,
			bookID, authorID, i,
		); err != nil {
			return err
		}
	}
	// Remove authors no longer referenced by any book.
	_, err := tx.Exec(`DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors)`)
	return err
}

func upsertTags(tx *sql.Tx, bookID int64, tags []string) error {
	if _, err := tx.Exec(`DELETE FROM book_tags WHERE book_id=?`, bookID); err != nil {
		return err
	}
	for _, tag := range tags {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO tags (name) VALUES (?)`, tag); err != nil {
			return err
		}
		var tagID int64
		if err := tx.QueryRow(`SELECT id FROM tags WHERE name=?`, tag).Scan(&tagID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO book_tags (book_id, tag_id) VALUES (?, ?)`, bookID, tagID); err != nil {
			return err
		}
	}
	// Remove tags no longer referenced by any book.
	_, err := tx.Exec(`DELETE FROM tags WHERE id NOT IN (SELECT tag_id FROM book_tags)`)
	return err
}

func ftsDelete(tx *sql.Tx, id int64, title, desc string) error {
	_, err := tx.Exec(
		`INSERT INTO books_fts(books_fts, rowid, title, description) VALUES('delete', ?, ?, ?)`,
		id, title, desc,
	)
	return err
}

func ftsInsert(tx *sql.Tx, id int64, title, desc string) error {
	_, err := tx.Exec(
		`INSERT INTO books_fts(rowid, title, description) VALUES(?, ?, ?)`,
		id, title, desc,
	)
	return err
}

// InsertBook inserts b into the index within tx.
func (idx *Index) InsertBook(tx *sql.Tx, b *model.Book) error {
	if _, err := tx.Exec(
		`INSERT INTO books
		    (id, title, sort_title, pubdate, description, language,
		     library_path, epub_filename, has_cover, status, rating,
		     date_added, date_modified)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.Meta.ID, b.Title, b.SortTitle, b.Pubdate, b.Description, b.Language,
		b.LibraryPath, b.EpubFilename, b.HasCover, b.Meta.Status, b.Meta.Rating,
		b.Meta.DateAdded.UTC().Format(time.RFC3339),
		b.Meta.DateModified.UTC().Format(time.RFC3339),
	); err != nil {
		return err
	}

	if err := upsertAuthors(tx, b.Meta.ID, b.Authors); err != nil {
		return err
	}

	if b.Series != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO series (name) VALUES (?)`, b.Series.Name); err != nil {
			return err
		}
		var seriesID int64
		if err := tx.QueryRow(`SELECT id FROM series WHERE name=?`, b.Series.Name).Scan(&seriesID); err != nil {
			return err
		}
		if _, err := tx.Exec(
			`UPDATE books SET series_id=?, series_index=? WHERE id=?`,
			seriesID, b.Series.Index, b.Meta.ID,
		); err != nil {
			return err
		}
	}

	if err := upsertTags(tx, b.Meta.ID, b.Meta.Tags); err != nil {
		return err
	}

	for scheme, value := range b.Identifiers {
		if _, err := tx.Exec(
			`INSERT INTO identifiers (book_id, scheme, value) VALUES (?, ?, ?)`,
			b.Meta.ID, scheme, value,
		); err != nil {
			return err
		}
	}

	return ftsInsert(tx, b.Meta.ID, b.Title, b.Description)
}

// UpdateBook replaces all index data for b.ID within tx. Used when the epub's
// internal OPF is rewritten and bibliographic fields change.
func (idx *Index) UpdateBook(tx *sql.Tx, b *model.Book) error {
	panic("not yet implemented")
}

// MoveBook updates the path and author/title fields for b.ID within tx.
// b should reflect the post-move state.
func (idx *Index) MoveBook(tx *sql.Tx, b *model.Book) error {
	// Read old values before modifying — needed to issue the FTS 'delete' command.
	var oldTitle, oldDesc string
	if err := tx.QueryRow(`SELECT title, description FROM books WHERE id=?`, b.Meta.ID).Scan(&oldTitle, &oldDesc); err != nil {
		return err
	}

	if _, err := tx.Exec(
		`UPDATE books SET library_path=?, epub_filename=?, title=?, sort_title=?, date_modified=? WHERE id=?`,
		b.LibraryPath, b.EpubFilename, b.Title, b.SortTitle,
		b.Meta.DateModified.UTC().Format(time.RFC3339), b.Meta.ID,
	); err != nil {
		return err
	}

	if err := upsertAuthors(tx, b.Meta.ID, b.Authors); err != nil {
		return err
	}

	// FTS content tables require explicit sync: delete old entry then insert new.
	if err := ftsDelete(tx, b.Meta.ID, oldTitle, oldDesc); err != nil {
		return err
	}
	return ftsInsert(tx, b.Meta.ID, b.Title, b.Description)
}

// UpdateMeta updates the sidecar fields (status, rating, tags, date_modified)
// for b.ID within tx.
func (idx *Index) UpdateMeta(tx *sql.Tx, b *model.Book) error {
	if _, err := tx.Exec(
		`UPDATE books SET status=?, rating=?, date_modified=? WHERE id=?`,
		b.Meta.Status, b.Meta.Rating, b.Meta.DateModified.UTC().Format(time.RFC3339), b.Meta.ID,
	); err != nil {
		return err
	}

	return upsertTags(tx, b.Meta.ID, b.Meta.Tags)
}

// DeleteBook removes all index rows for id within tx.
func (idx *Index) DeleteBook(tx *sql.Tx, id int64) error {
	var title, desc string
	if err := tx.QueryRow(`SELECT title, description FROM books WHERE id=?`, id).Scan(&title, &desc); err != nil {
		return err
	}

	if err := ftsDelete(tx, id, title, desc); err != nil {
		return err
	}

	// ON DELETE CASCADE handles book_authors, book_tags, identifiers.
	if _, err := tx.Exec(`DELETE FROM books WHERE id=?`, id); err != nil {
		return err
	}

	orphanCleanup := []string{
		`DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors)`,
		`DELETE FROM series  WHERE id NOT IN (SELECT series_id  FROM books WHERE series_id IS NOT NULL)`,
		`DELETE FROM tags    WHERE id NOT IN (SELECT tag_id     FROM book_tags)`,
	}
	for _, q := range orphanCleanup {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}

	return nil
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
