package index

import (
	"database/sql"
	"errors"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

const bookColumns = `(id, title, sort_title, pubdate, description, language,
		     library_path, epub_filename, cover_path, status, rating,
		     date_added, date_modified, opf_size, cover_size, epub_size)`

func bookValues(b *model.Book) []any {
	sortTitle := any(b.SortTitle)
	if sortTitle == "" {
		sortTitle = nil
	}
	return []any{
		b.Meta.ID, b.Title, sortTitle, b.Pubdate, b.Description, b.Language,
		b.LibraryPath, b.EpubFilename, b.CoverPath, b.Meta.Status, b.Meta.Rating,
		b.Meta.DateAdded.UTC().Format(time.RFC3339),
		b.Meta.DateModified.UTC().Format(time.RFC3339),
		b.OpfSize, b.CoverSize, b.EpubSize,
	}
}

// insertBook inserts a new book row, failing on id conflict — used by Rebuild.
//
// It deliberately skips cleanupOrphans: Rebuild empties every table before the
// insert loop, so each author/series/tag is written alongside the book that
// references it and nothing can be orphaned. Sweeping per book would run three
// growing anti-join scans N times for no effect.
func insertBook(tx *sql.Tx, b *model.Book) error {
	// series_id/series_index are set by finishBook's upsertSeries.
	if _, err := tx.Exec(
		`INSERT INTO books `+bookColumns+` VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		bookValues(b)...,
	); err != nil {
		return err
	}

	return finishBook(tx, b)
}

// putBook inserts or replaces b, using ON CONFLICT to update an existing row.
// Rebuild, which must surface id collisions, uses insertBook instead.
func putBook(tx *sql.Tx, b *model.Book) error {
	// series_id/series_index are set by finishBook's upsertSeries.
	if _, err := tx.Exec(
		`INSERT INTO books `+bookColumns+` VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     title=excluded.title, sort_title=excluded.sort_title, pubdate=excluded.pubdate,
		     description=excluded.description, language=excluded.language,
		     library_path=excluded.library_path, epub_filename=excluded.epub_filename,
		     cover_path=excluded.cover_path, status=excluded.status, rating=excluded.rating,
		     date_added=excluded.date_added, date_modified=excluded.date_modified,
		     opf_size=excluded.opf_size, cover_size=excluded.cover_size, epub_size=excluded.epub_size`,
		bookValues(b)...,
	); err != nil {
		return err
	}

	if err := finishBook(tx, b); err != nil {
		return err
	}
	// Replacing a book can strand its former author/series/tag rows.
	return cleanupOrphans(tx)
}

// finish runs fn and clears the pending row in one transaction. MarkPending
// must have been called first so a pending row protects the preceding store
// writes; the row is atomically deleted inside the same transaction.
func (o *Op) finish(fn func(*sql.Tx) error) error {
	if o.opID == "" {
		return errors.New("MarkPending must be called before commit")
	}
	return o.idx.withTx(func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		_, err := tx.Exec("DELETE FROM pending_ops WHERE op_id = ?", o.opID)
		return err
	})
}

// Put writes b into the index, inserting or replacing the record for b.Meta.ID.
func (o *Op) Put(b *model.Book) error {
	return o.finish(func(tx *sql.Tx) error { return putBook(tx, b) })
}

// Delete removes all index rows for book.
func (o *Op) Delete(bookID int64) error {
	return o.finish(func(tx *sql.Tx) error { return deleteBook(tx, bookID) })
}

// finishBook writes a book's authors, tags, series, and identifiers. It does not
// sweep orphans — callers that can strand rows (putBook, deleteBook) call
// cleanupOrphans themselves.
func finishBook(tx *sql.Tx, b *model.Book) error {
	if err := upsertAuthors(tx, b.Meta.ID, b.Authors); err != nil {
		return err
	}
	if err := upsertTags(tx, b.Meta.ID, b.Meta.Tags); err != nil {
		return err
	}
	if err := upsertSeries(tx, b); err != nil {
		return err
	}

	if _, err := tx.Exec(`DELETE FROM identifiers WHERE book_id=?`, b.Meta.ID); err != nil {
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
	return nil
}

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
	return nil
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
	return nil
}

// upsertSeries points the book at its series or clears series_id, then removes
// orphaned series rows. It must run after the books row exists.
func upsertSeries(tx *sql.Tx, b *model.Book) error {
	var seriesID, seriesIndex any
	if b.Series != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO series (name) VALUES (?)`, b.Series.Name); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(`SELECT id FROM series WHERE name=?`, b.Series.Name).Scan(&id); err != nil {
			return err
		}
		seriesID, seriesIndex = id, b.Series.Index
	}

	if _, err := tx.Exec(
		`UPDATE books SET series_id=?, series_index=? WHERE id=?`,
		seriesID, seriesIndex, b.Meta.ID,
	); err != nil {
		return err
	}

	return nil
}

func deleteBook(tx *sql.Tx, id int64) error {
	// ON DELETE CASCADE handles book_authors, book_tags, identifiers.
	if _, err := tx.Exec(`DELETE FROM books WHERE id=?`, id); err != nil {
		return err
	}
	return cleanupOrphans(tx)
}

// cleanupOrphans removes authors, series, and tags that are no longer
// referenced by any book.
func cleanupOrphans(tx *sql.Tx) error {
	queries := []string{
		`DELETE FROM authors WHERE id NOT IN (SELECT author_id FROM book_authors)`,
		`DELETE FROM series  WHERE id NOT IN (SELECT series_id  FROM books WHERE series_id IS NOT NULL)`,
		`DELETE FROM tags    WHERE id NOT IN (SELECT tag_id     FROM book_tags)`,
	}
	for _, q := range queries {
		if _, err := tx.Exec(q); err != nil {
			return err
		}
	}
	return nil
}
