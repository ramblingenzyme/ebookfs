package index

import (
	"database/sql"
	"fmt"
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

// AllPathInfo returns every library path the last rebuild accounted for, mapped
// to the file state recorded for it — both indexed books and the directories it
// could not index. Drift detection compares a store listing against this, so a
// path missing here is genuinely unexplained rather than merely unindexable.
func (idx *Index) AllPathInfo() (map[string]PathInfo, error) {
	rows, err := idx.db.Query(`
		SELECT ` + pathInfoSelect + ` FROM books
		UNION ALL
		SELECT ` + pathInfoSelect + ` FROM skipped_books`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	info := make(map[string]PathInfo)
	for rows.Next() {
		var path, epubName string
		var size, epubMtime, metaMtime, metaSize int64
		if err := rows.Scan(&path, &epubName, &size, &epubMtime, &metaMtime, &metaSize); err != nil {
			return nil, err
		}
		// books and skipped_books are disjoint by construction (Rebuild puts
		// each walked directory in exactly one), but nothing in the schema
		// enforces it across tables. A path in both would collapse in this map
		// and silently satisfy the caller's count comparison, masking real
		// drift — so refuse rather than return a half-truth.
		if _, dup := info[path]; dup {
			return nil, fmt.Errorf("index inconsistency: %q recorded as both indexed and skipped", path)
		}
		info[path] = PathInfo{
			EpubFilename: epubName,
			Size:         size,
			EpubMtime:    fromUnixNano(epubMtime),
			MetaSize:     metaSize,
			MetaMtime:    fromUnixNano(metaMtime),
		}
	}
	return info, rows.Err()
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

// ListAuthors returns all authors in the index, ordered by sort_name.
func (idx *Index) ListAuthors() ([]*model.Author, error) {
	panic("not yet implemented")
}

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*model.Stats, error) {
	var s model.Stats
	var totalSize sql.NullInt64
	var lastAdded, lastModified sql.NullString
	err := idx.db.QueryRow(`
		SELECT COUNT(*), COALESCE(SUM(epub_size), 0), MAX(date_added), MAX(date_modified)
		FROM books
	`).Scan(&s.Books, &totalSize, &lastAdded, &lastModified)
	if err != nil {
		return nil, err
	}
	s.TotalSize = totalSize.Int64
	if lastAdded.Valid {
		s.LastAdded, _ = time.Parse(time.RFC3339, lastAdded.String)
	}
	if lastModified.Valid {
		s.LastModified, _ = time.Parse(time.RFC3339, lastModified.String)
	}

	if err := idx.db.QueryRow(`SELECT COUNT(*) FROM authors`).Scan(&s.Authors); err != nil {
		return nil, err
	}
	if err := idx.db.QueryRow(`SELECT COUNT(*) FROM series`).Scan(&s.Series); err != nil {
		return nil, err
	}
	if err := idx.db.QueryRow(`SELECT COUNT(*) FROM tags`).Scan(&s.Tags); err != nil {
		return nil, err
	}
	return &s, nil
}
