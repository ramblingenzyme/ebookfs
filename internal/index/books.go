package index

import (
	"database/sql"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Filter selects a subset of books for Query. Zero-valued fields are ignored,
// so Filter{} matches every book. String fields match by exact name.
type Filter struct {
	ID     int64  // a single book by id
	Author string // books by an author's name
	Tag    string // books carrying a tag
	Status string // books with a reading status
	Series string // books in a series
	Recent bool   // order by date added (newest first) instead of sort title
	Limit  int    // cap the result count; 0 means no limit
}

// Query returns the books matching f, each hydrated with authors, tags, and
// identifiers. Every book-listing view (all books, by author, by tag, recent,
// …) is expressed as a Filter rather than its own bespoke method.
func (idx *Index) Query(f Filter) ([]*model.Book, error) {
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

// ListAll returns all books ordered by sort_title.
func (idx *Index) ListAll() ([]*model.Book, error) {
	return idx.Query(Filter{})
}

// Get returns the book with the given id, or sql.ErrNoRows if it is absent.
func (idx *Index) Get(bookID int64) (*model.Book, error) {
	books, err := idx.Query(Filter{ID: bookID})
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, sql.ErrNoRows
	}
	return books[0], nil
}

// queryBooks runs the shared book SELECT with an optional WHERE/ORDER/LIMIT and
// hydrates the per-book authors, tags, and identifiers.
func (idx *Index) queryBooks(where string, args []any, order string, limit int) ([]*model.Book, error) {
	q := `
		SELECT b.id, b.title, b.sort_title, COALESCE(b.pubdate, ''), b.description, b.language,
		       b.library_path, b.epub_filename, b.has_cover,
		       b.status, b.rating, b.date_added, b.date_modified,
		       s.id, s.name, b.series_index
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

// Put writes b into the index, inserting it or fully replacing the existing
// record for b.Meta.ID. It is the index's single write primitive: ingest, move,
// sidecar edits, and reindex all reduce to "make the index reflect this book".
// Pair it with DeleteBook.
func (idx *Index) Put(b *model.Book) error {
	return idx.withTx(func(tx *sql.Tx) error { return putBook(tx, b) })
}

func putBook(tx *sql.Tx, b *model.Book) error {
	// Resolve the series row first so its id (or NULL) goes straight into the
	// upsert. seriesID/seriesIndex stay nil — and thus SQL NULL — when the book
	// has no series, which also clears a series the book has just left.
	var seriesID, seriesIndex any
	if b.Series != nil {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO series (name) VALUES (?)`, b.Series.Name); err != nil {
			return err
		}
		var id int64
		if err := tx.QueryRow(`SELECT id FROM series WHERE name=?`, b.Series.Name).Scan(&id); err != nil {
			return err
		}
		seriesID = id
		seriesIndex = b.Series.Index
	}

	if _, err := tx.Exec(
		`INSERT INTO books
		    (id, title, sort_title, pubdate, description, language,
		     library_path, epub_filename, has_cover, status, rating,
		     date_added, date_modified, series_id, series_index)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		     title=excluded.title, sort_title=excluded.sort_title, pubdate=excluded.pubdate,
		     description=excluded.description, language=excluded.language,
		     library_path=excluded.library_path, epub_filename=excluded.epub_filename,
		     has_cover=excluded.has_cover, status=excluded.status, rating=excluded.rating,
		     date_added=excluded.date_added, date_modified=excluded.date_modified,
		     series_id=excluded.series_id, series_index=excluded.series_index`,
		b.Meta.ID, b.Title, b.SortTitle, b.Pubdate, b.Description, b.Language,
		b.LibraryPath, b.EpubFilename, b.HasCover, b.Meta.Status, b.Meta.Rating,
		b.Meta.DateAdded.UTC().Format(time.RFC3339),
		b.Meta.DateModified.UTC().Format(time.RFC3339),
		seriesID, seriesIndex,
	); err != nil {
		return err
	}

	if err := upsertAuthors(tx, b.Meta.ID, b.Authors); err != nil {
		return err
	}
	if err := upsertTags(tx, b.Meta.ID, b.Meta.Tags); err != nil {
		return err
	}

	// Replace identifiers wholesale.
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

	// A book leaving a series can orphan the old series row (authors and tags are
	// already cleaned by upsertAuthors/upsertTags).
	_, err := tx.Exec(`DELETE FROM series WHERE id NOT IN (SELECT series_id FROM books WHERE series_id IS NOT NULL)`)
	return err
}

// Delete removes all index rows for bookID.
func (idx *Index) Delete(bookID int64) error {
	return idx.withTx(func(tx *sql.Tx) error { return deleteBook(tx, bookID) })
}

func deleteBook(tx *sql.Tx, id int64) error {
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

// ListAuthors returns all authors in the index, ordered by sort_name.
func (idx *Index) ListAuthors() ([]*model.Author, error) {
	panic("not yet implemented")
}

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*model.Stats, error) {
	panic("not yet implemented")
}
