package index

import (
	"database/sql"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// ListAll returns all books ordered by sort_title.
func (idx *Index) ListAll() ([]*model.Book, error) {
	rows, err := idx.db.Query(`
		SELECT b.id, b.title, b.sort_title, COALESCE(b.pubdate, ''), b.description, b.language,
		       b.library_path, b.epub_filename, b.has_cover,
		       b.status, b.rating, b.date_added, b.date_modified,
		       s.id, s.name, b.series_index
		FROM books b
		LEFT JOIN series s ON s.id = b.series_id
		ORDER BY b.sort_title
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []*model.Book
	byID := make(map[int64]*model.Book)
	for rows.Next() {
		b := &model.Book{Identifiers: make(map[string]string)}
		var hasCover int
		var dateAdded, dateModified string
		var seriesID sql.NullInt64
		var seriesName sql.NullString
		var seriesIndex sql.NullFloat64
		if err := rows.Scan(
			&b.Meta.ID, &b.Title, &b.SortTitle, &b.Pubdate, &b.Description, &b.Language,
			&b.LibraryPath, &b.EpubFilename, &hasCover,
			&b.Meta.Status, &b.Meta.Rating, &dateAdded, &dateModified,
			&seriesID, &seriesName, &seriesIndex,
		); err != nil {
			return nil, err
		}
		b.HasCover = hasCover != 0
		b.Meta.DateAdded, _ = time.Parse(time.RFC3339, dateAdded)
		b.Meta.DateModified, _ = time.Parse(time.RFC3339, dateModified)
		if seriesName.Valid {
			b.Series = &model.SeriesRef{ID: seriesID.Int64, Name: seriesName.String, Index: seriesIndex.Float64}
		}
		books = append(books, b)
		byID[b.Meta.ID] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := idx.loadAuthors(byID); err != nil {
		return nil, err
	}
	if err := idx.loadTags(byID); err != nil {
		return nil, err
	}
	if err := idx.loadIdentifiers(byID); err != nil {
		return nil, err
	}
	return books, nil
}

func (idx *Index) loadAuthors(byID map[int64]*model.Book) error {
	rows, err := idx.db.Query(`
		SELECT ba.book_id, a.id, a.name, a.sort_name
		FROM book_authors ba
		JOIN authors a ON a.id = ba.author_id
		ORDER BY ba.book_id, ba.position
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bookID int64
		var a model.Author
		if err := rows.Scan(&bookID, &a.ID, &a.Name, &a.SortName); err != nil {
			return err
		}
		if b, ok := byID[bookID]; ok {
			b.Authors = append(b.Authors, a)
		}
	}
	return rows.Err()
}

func (idx *Index) loadTags(byID map[int64]*model.Book) error {
	rows, err := idx.db.Query(`
		SELECT bt.book_id, t.name
		FROM book_tags bt
		JOIN tags t ON t.id = bt.tag_id
		ORDER BY bt.book_id, t.name
	`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bookID int64
		var tag string
		if err := rows.Scan(&bookID, &tag); err != nil {
			return err
		}
		if b, ok := byID[bookID]; ok {
			b.Meta.Tags = append(b.Meta.Tags, tag)
		}
	}
	return rows.Err()
}

func (idx *Index) loadIdentifiers(byID map[int64]*model.Book) error {
	rows, err := idx.db.Query(`SELECT book_id, scheme, value FROM identifiers`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var bookID int64
		var scheme, value string
		if err := rows.Scan(&bookID, &scheme, &value); err != nil {
			return err
		}
		if b, ok := byID[bookID]; ok {
			b.Identifiers[scheme] = value
		}
	}
	return rows.Err()
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

// InsertBook inserts b into the index.
func (idx *Index) InsertBook(b *model.Book) error {
	return idx.withTx(func(tx *sql.Tx) error { return insertBook(tx, b) })
}

func insertBook(tx *sql.Tx, b *model.Book) error {
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

// UpdateBook replaces all index data for b.ID. Used when the epub's internal OPF
// is rewritten and bibliographic fields change.
func (idx *Index) UpdateBook(b *model.Book) error {
	panic("not yet implemented")
}

// MoveBook updates the path and author/title fields for b.ID.
// b should reflect the post-move state.
func (idx *Index) MoveBook(b *model.Book) error {
	return idx.withTx(func(tx *sql.Tx) error { return moveBook(tx, b) })
}

func moveBook(tx *sql.Tx, b *model.Book) error {
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
// for b.ID.
func (idx *Index) UpdateMeta(b *model.Book) error {
	return idx.withTx(func(tx *sql.Tx) error { return updateMeta(tx, b) })
}

func updateMeta(tx *sql.Tx, b *model.Book) error {
	if _, err := tx.Exec(
		`UPDATE books SET status=?, rating=?, date_modified=? WHERE id=?`,
		b.Meta.Status, b.Meta.Rating, b.Meta.DateModified.UTC().Format(time.RFC3339), b.Meta.ID,
	); err != nil {
		return err
	}

	return upsertTags(tx, b.Meta.ID, b.Meta.Tags)
}

// DeleteBook removes all index rows for id.
func (idx *Index) DeleteBook(id int64) error {
	return idx.withTx(func(tx *sql.Tx) error { return deleteBook(tx, id) })
}

func deleteBook(tx *sql.Tx, id int64) error {
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
