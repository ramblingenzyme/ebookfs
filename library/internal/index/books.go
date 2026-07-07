package index

import (
	"database/sql"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Query returns the books matching f, each hydrated with authors, tags, and
// identifiers. Every book-listing view (all books, by author, by tag, recent,
// …) is expressed as a Filter rather than its own bespoke method.
func (idx *Index) Query(f model.Filter) ([]*model.Book, error) {
	// Each row is one optional predicate: include its expr/arg when `on` holds.
	// Values are always parameterized; only the fixed expressions are literal.
	conds := []struct {
		on   bool
		expr string
		arg  any
	}{
		{f.ID != 0, "b.id = ?", f.ID},
		{f.Status != "", "b.status = ?", f.Status},
		{f.Author != "", "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name = ?)", f.Author},
		{f.Tag != "", "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name = ?)", f.Tag},
		{f.Series != "", "b.series_id IN (SELECT id FROM series WHERE name = ?)", f.Series},
	}

	var (
		where []string
		args  []any
	)
	for _, c := range conds {
		if c.on {
			where = append(where, c.expr)
			args = append(args, c.arg)
		}
	}

	order := "b.sort_title"
	if f.Recent {
		order = "b.date_added DESC"
	}

	return idx.queryBooks(strings.Join(where, " AND "), args, order, f.Limit)
}

// Get returns the book with the given id, or sql.ErrNoRows if it is absent.
func (idx *Index) Get(bookID int64) (*model.Book, error) {
	books, err := idx.Query(model.Filter{ID: bookID})
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, sql.ErrNoRows
	}
	return books[0], nil
}

// queryBooks runs the shared book SELECT with optional WHERE/ORDER/LIMIT and
// hydrates per-book authors, tags, and identifiers.
func (idx *Index) queryBooks(where string, args []any, order string, limit int) ([]*model.Book, error) {
	q := `
		SELECT b.id, b.title, b.sort_title, COALESCE(b.pubdate, ''), b.description, b.language,
		       b.library_path, b.epub_filename, b.cover_path,
		       b.status, b.rating, b.date_added, b.date_modified,
		       s.id, s.name, b.series_index,
		       b.opf_size, b.cover_size, b.epub_size
		FROM books b
		LEFT JOIN series s ON s.id = b.series_id`
	if where != "" {
		q += "\n\t\tWHERE " + where
	}
	q += "\n\t\tORDER BY " + order
	if limit > 0 {
		q += "\n\t\tLIMIT ?"
		args = append(args, limit)
	}

	rows, err := idx.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var books []*model.Book
	byID := make(map[int64]*model.Book)
	for rows.Next() {
		b := &model.Book{Bib: model.Bib{Identifiers: make(map[string]string)}}
		var dateAdded, dateModified string
		var sortTitle sql.NullString
		var seriesID sql.NullInt64
		var seriesName sql.NullString
		var seriesIndex sql.NullFloat64
		if err := rows.Scan(
			&b.Meta.ID, &b.Title, &sortTitle, &b.Pubdate, &b.Description, &b.Language,
			&b.LibraryPath, &b.EpubFilename, &b.CoverPath,
			&b.Meta.Status, &b.Meta.Rating, &dateAdded, &dateModified,
			&seriesID, &seriesName, &seriesIndex,
			&b.OpfSize, &b.CoverSize, &b.EpubSize,
		); err != nil {
			return nil, err
		}
		b.Meta.DateAdded, _ = time.Parse(time.RFC3339, dateAdded)
		b.Meta.DateModified, _ = time.Parse(time.RFC3339, dateModified)
		b.SortTitle = sortTitle.String
		if seriesName.Valid {
			b.Series = &model.SeriesRef{ID: seriesID.Int64, Name: seriesName.String, Index: seriesIndex.Float64}
		}
		books = append(books, b)
		byID[b.Meta.ID] = b
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Nothing matched: skip the child queries entirely rather than scanning
	// three tables to hydrate zero books.
	if len(byID) == 0 {
		return books, nil
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

// maxScopeIDs caps how many ids scopeByBook binds into an IN clause before
// falling back to a full table scan. 900 is safely below every SQLite build's
// SQLITE_MAX_VARIABLE_NUMBER and past the point IN beats a scan anyway.
const maxScopeIDs = 900

// scopeByBook returns a "WHERE <col> IN (…)" clause scoped to byID, or ("", nil)
// for a full scan when there are too many ids.
func scopeByBook(col string, byID map[int64]*model.Book) (string, []any) {
	if len(byID) == 0 || len(byID) > maxScopeIDs {
		return "", nil
	}
	placeholders := make([]string, 0, len(byID))
	args := make([]any, 0, len(byID))
	for id := range byID {
		placeholders = append(placeholders, "?")
		args = append(args, id)
	}
	return " WHERE " + col + " IN (" + strings.Join(placeholders, ",") + ")", args
}

func (idx *Index) loadAuthors(byID map[int64]*model.Book) error {
	where, args := scopeByBook("ba.book_id", byID)
	rows, err := idx.db.Query(`
		SELECT ba.book_id, a.id, a.name, a.sort_name
		FROM book_authors ba
		JOIN authors a ON a.id = ba.author_id`+where+`
		ORDER BY ba.book_id, ba.position
	`, args...)
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
	where, args := scopeByBook("bt.book_id", byID)
	rows, err := idx.db.Query(`
		SELECT bt.book_id, t.name
		FROM book_tags bt
		JOIN tags t ON t.id = bt.tag_id`+where+`
		ORDER BY bt.book_id, t.name
	`, args...)
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
	where, args := scopeByBook("book_id", byID)
	rows, err := idx.db.Query(`SELECT book_id, scheme, value FROM identifiers`+where, args...)
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

// Put writes b into the index, inserting or replacing the record for b.Meta.ID.
// The optional storeWrite runs between setDirty and the index write so a crash
// during the store write forces a reindex on the next startup.
func (idx *Index) Put(b *model.Book, storeWrite func() error) error {
	return idx.withTx(func(tx *sql.Tx) error {
		if storeWrite != nil {
			if err := storeWrite(); err != nil {
				return err
			}
		}
		return putBook(tx, b)
	})
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

// finishBook writes the per-book child rows after the books row exists.
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
	return cleanupOrphans(tx)
}

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

	return finishBook(tx, b)
}

// Delete removes all index rows for bookID. The optional storeWrite runs
// between setDirty and the index delete (see Put).
func (idx *Index) Delete(bookID int64, storeWrite func() error) error {
	return idx.withTx(func(tx *sql.Tx) error {
		if storeWrite != nil {
			if err := storeWrite(); err != nil {
				return err
			}
		}
		return deleteBook(tx, bookID)
	})
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

// ListAuthors returns all authors in the index, ordered by sort_name.
func (idx *Index) ListAuthors() ([]*model.Author, error) {
	panic("not yet implemented")
}

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*model.Stats, error) {
	panic("not yet implemented")
}
