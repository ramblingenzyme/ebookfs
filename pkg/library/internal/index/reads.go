package index

import (
	"database/sql"
	"fmt"
	"log/slog"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

func (idx *Index) queryBooks(q *bookQuery) ([]*book.Book, error) {
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
func (idx *Index) Get(bookID int64) (*book.Book, error) {
	books, err := idx.Search(Query{IDs: []int64{bookID}})
	if err != nil {
		return nil, err
	}
	if len(books) == 0 {
		return nil, sql.ErrNoRows
	}
	return books[0], nil
}

// Stats returns aggregate library statistics.
func (idx *Index) Stats() (*Stats, error) {
	stats, err := idx.queries.GetStats(idx.ctx)
	if err != nil {
		return nil, err
	}

	s := &Stats{
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

// ListAuthors returns every author with at least one book, ordered by sort
// name. ListSeries and ListTags are the same by their own name.
func (idx *Index) ListAuthors() ([]Facet, error) {
	rows, err := idx.queries.ListAuthors(idx.ctx)
	return facets(rows, err, func(r dbsqlc.ListAuthorsRow) (string, int64) { return r.Name, r.BookCount })
}

func (idx *Index) ListSeries() ([]Facet, error) {
	rows, err := idx.queries.ListSeries(idx.ctx)
	return facets(rows, err, func(r dbsqlc.ListSeriesRow) (string, int64) { return r.Name, r.BookCount })
}

func (idx *Index) ListTags() ([]Facet, error) {
	rows, err := idx.queries.ListTags(idx.ctx)
	return facets(rows, err, func(r dbsqlc.ListTagsRow) (string, int64) { return r.Name, r.BookCount })
}

// facets converts a name-and-count row set into Facets, preserving the SQL
// ordering. The three list queries differ only in their generated row type,
// which the type parameter erases.
func facets[R any](rows []R, err error, split func(R) (string, int64)) ([]Facet, error) {
	if err != nil {
		return nil, err
	}
	out := make([]Facet, len(rows))
	for i, r := range rows {
		name, n := split(r)
		out[i] = Facet{Name: name, Count: int(n)}
	}
	return out, nil
}
