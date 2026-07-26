package index

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Query returns the books matching f, each hydrated with authors, tags, and
// identifiers. Every book-listing view (all books, by author, by tag, recent,
// …) is expressed as a Filter rather than its own bespoke method.
func (idx *Index) Query(f model.Filter) ([]*model.Book, error) {
	conds := []struct {
		on   bool
		expr string
		args []any
	}{
		{f.ID != 0, "b.id = ?", []any{f.ID}},
		{f.Status != "", "b.status = ?", []any{f.Status}},
		{f.Author != "", "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name = ? OR a.sort_name = ?)", []any{f.Author, f.Author}},
		{f.Tag != "", "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name = ?)", []any{f.Tag}},
		{f.Series != "", "b.series_id IN (SELECT id FROM series WHERE name = ?)", []any{f.Series}},
	}

	var (
		where []string
		args  []any
	)
	for _, c := range conds {
		if c.on {
			where = append(where, c.expr)
			args = append(args, c.args...)
		}
	}

	order := "b.sort_title"
	if f.Recent {
		order = "b.date_added DESC"
	}

	return idx.queryBooks(strings.Join(where, " AND "), args, order, f.Limit)
}

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

	rows, err := idx.readDB.QueryContext(idx.ctx, q, args...)
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
		if t, err := time.Parse(time.RFC3339, dateAdded); err != nil {
			log.Printf("book %d: invalid date_added %q: %v", b.Meta.ID, dateAdded, err)
		} else {
			b.Meta.DateAdded = t
		}
		if t, err := time.Parse(time.RFC3339, dateModified); err != nil {
			log.Printf("book %d: invalid date_modified %q: %v", b.Meta.ID, dateModified, err)
		} else {
			b.Meta.DateModified = t
		}
		b.SortTitle = sortTitle.String
		if seriesName.Valid {
			b.Series = &model.SeriesRef{ID: seriesID.Int64, Name: seriesName.String, Index: seriesIndex.Float64}
		}
		books = append(books, b)
		byID[b.Meta.ID] = b
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(byID) == 0 {
		return books, nil
	}

	ids := make([]int64, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}

	authorRows, err := idx.queries.GetAuthorsByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	bookAuthors := make(map[int64][]model.Author, len(ids))
	for _, row := range authorRows {
		bookAuthors[row.BookID] = append(bookAuthors[row.BookID], model.Author{
			ID:       row.ID,
			Name:     row.Name,
			SortName: row.SortName,
		})
	}

	tagRows, err := idx.queries.GetTagsByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	bookTags := make(map[int64][]string, len(ids))
	for _, row := range tagRows {
		bookTags[row.BookID] = append(bookTags[row.BookID], row.Name)
	}

	idRows, err := idx.queries.GetIdentifiersByBookIDs(idx.ctx, ids)
	if err != nil {
		return nil, err
	}
	bookIDs := make(map[int64]map[string]string, len(ids))
	for _, row := range idRows {
		m := bookIDs[row.BookID]
		if m == nil {
			m = make(map[string]string)
			bookIDs[row.BookID] = m
		}
		m[row.Scheme] = row.Value
	}

	for _, b := range books {
		b.Authors = bookAuthors[b.Meta.ID]
		if b.Authors == nil {
			b.Authors = []model.Author{}
		}
		b.Meta.Tags = bookTags[b.Meta.ID]
		if b.Meta.Tags == nil {
			b.Meta.Tags = []string{}
		}
		if m := bookIDs[b.Meta.ID]; m != nil {
			b.Identifiers = m
		} else {
			b.Identifiers = make(map[string]string)
		}
	}

	return books, nil
}

// AllPathInfo returns every library path the last rebuild accounted for, mapped
// to the file state recorded for it — both indexed books and the directories it
// could not index. Drift detection compares a store listing against this, so a
// path missing here is genuinely unexplained rather than merely unindexable.
func (idx *Index) AllPathInfo() (map[string]drift.PathInfo, error) {
	rows, err := idx.queries.GetAllPathInfo(idx.ctx)
	if err != nil {
		return nil, err
	}

	info := make(map[string]drift.PathInfo)
	for _, row := range rows {
		// books and skipped_books are disjoint by construction (Rebuild puts
		// each walked directory in exactly one), but nothing in the schema
		// enforces it across tables. A path in both would collapse in this map
		// and silently satisfy the caller's count comparison, masking real
		// drift — so refuse rather than return a half-truth.
		if _, dup := info[row.LibraryPath]; dup {
			return nil, fmt.Errorf("index inconsistency: %q recorded as both indexed and skipped", row.LibraryPath)
		}
		info[row.LibraryPath] = drift.PathInfo{
			EpubFilename: row.EpubFilename,
			Size:         row.EpubSize,
			EpubMtime:    fromUnixNano(row.EpubMtime),
			MetaSize:     row.MetaSize,
			MetaMtime:    fromUnixNano(row.MetaMtime),
		}
	}
	return info, nil
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

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*model.Stats, error) {
	stats, err := idx.queries.GetStats(idx.ctx)
	if err != nil {
		return nil, err
	}

	s := &model.Stats{
		Books:     int(stats.Books),
		Authors:   int(stats.Authors),
		Series:    int(stats.Series),
		Tags:      int(stats.Tags),
		TotalSize: stats.TotalSize,
	}

	if dateStr, ok := stats.LastAdded.(string); ok && dateStr != "" {
		s.LastAdded, _ = time.Parse(time.RFC3339, dateStr)
	}
	if dateStr, ok := stats.LastModified.(string); ok && dateStr != "" {
		s.LastModified, _ = time.Parse(time.RFC3339, dateStr)
	}

	return s, nil
}
