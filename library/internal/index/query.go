package index

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Query returns the books matching f, each hydrated with authors, tags, and
// identifiers. Every book-listing view (all books, by author, by tag, recent,
// …) is expressed as a Filter rather than its own bespoke method.
func (idx *Index) Query(f model.Filter) ([]*model.Book, error) {
	q := &bookQuery{order: "b.sort_title"}
	if f.Recent {
		q.order = "b.date_added DESC"
	}
	q.limit = f.Limit

	q.addCondition(f.ID != 0, "b.id = ?", f.ID)
	q.addCondition(f.Status != "", "b.status = ?", f.Status)
	q.addCondition(f.Author != "", "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name = ? OR a.sort_name = ?)", f.Author, f.Author)
	q.addCondition(f.Tag != "", "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name = ?)", f.Tag)
	q.addCondition(f.Series != "", "b.series_id IN (SELECT id FROM series WHERE name = ?)", f.Series)

	return idx.queryBooks(q)
}

func (idx *Index) queryBooks(q *bookQuery) ([]*model.Book, error) {
	sql, args := q.sql()
	rows, err := idx.readDB.QueryContext(idx.ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	books, err := scanBookRows(rows)
	if err != nil || len(books) == 0 {
		return books, err
	}

	return books, idx.hydrateBooks(books)
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
		if _, dup := info[row.EpubPath]; dup {
			return nil, fmt.Errorf("index inconsistency: %q recorded as both indexed and skipped", row.EpubPath)
		}
		info[row.EpubPath] = drift.PathInfo{
			Size:      row.EpubSize,
			EpubMtime: fromUnixNano(row.EpubMtime),
			MetaSize:  row.MetaSize,
			MetaMtime: fromUnixNano(row.MetaMtime),
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
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			slog.Warn("stats: invalid last_added", "last_added", dateStr, "error", err)
		} else {
			s.LastAdded = t
		}
	}
	if dateStr, ok := stats.LastModified.(string); ok && dateStr != "" {
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			slog.Warn("stats: invalid last_modified", "last_modified", dateStr, "error", err)
		} else {
			s.LastModified = t
		}
	}

	return s, nil
}
