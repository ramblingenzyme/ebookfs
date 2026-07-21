package index

import (
	"database/sql"
	"fmt"
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

	rows, err := idx.db.QueryContext(idx.ctx, q, args...)
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

	if len(byID) == 0 {
		return books, nil
	}

	for _, b := range books {
		authors, err := idx.queries.GetAuthorsByBookID(idx.ctx, b.Meta.ID)
		if err != nil {
			return nil, err
		}
		b.Authors = make([]model.Author, len(authors))
		for i, a := range authors {
			b.Authors[i] = model.Author{
				ID:       a.ID,
				Name:     a.Name,
				SortName: a.SortName,
			}
		}

		tags, err := idx.queries.GetTagsByBookID(idx.ctx, b.Meta.ID)
		if err != nil {
			return nil, err
		}
		b.Meta.Tags = tags

		identifiers, err := idx.queries.GetIdentifiersByBookID(idx.ctx, b.Meta.ID)
		if err != nil {
			return nil, err
		}
		b.Identifiers = make(map[string]string)
		for _, id := range identifiers {
			b.Identifiers[id.Scheme] = id.Value
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

// ListAuthors returns all authors in the index, ordered by sort_name.
func (idx *Index) ListAuthors() ([]*model.Author, error) {
	authors, err := idx.queries.ListAuthors(idx.ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*model.Author, len(authors))
	for i, a := range authors {
		result[i] = &model.Author{
			ID:       a.ID,
			Name:     a.Name,
			SortName: a.SortName,
		}
	}
	return result, nil
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
