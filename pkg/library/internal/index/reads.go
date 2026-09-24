package index

import (
	"database/sql"
	"fmt"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

func (idx *Index) queryBooks(q *bookQuery) ([]*book.Book, error) {
	query, args := q.sql()
	rows, err := idx.readDB.QueryContext(idx.ctx, query, args...)
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

// AllPathInfo covers the directories the rebuild could not index as well as
// the books it did. Drift detection compares a store listing against it, so a
// path missing here is unexplained rather than merely unindexable.
func (idx *Index) AllPathInfo() (map[string]drift.PathInfo, error) {
	rows, err := idx.queries.GetAllPathInfo(idx.ctx)
	if err != nil {
		return nil, err
	}

	info := make(map[string]drift.PathInfo)
	for _, row := range rows {
		// Rebuild puts each walked directory in exactly one of books and
		// skipped_books, and nothing in the schema enforces that across tables.
		// A path in both collapses in this map and still satisfies the caller's
		// count comparison, masking real drift.
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

	// The aggregate columns come back as any, since MAX over a text column has
	// no declared type, and are absent from an empty library.
	if dateStr, ok := stats.LastAdded.(string); ok && dateStr != "" {
		s.LastAdded = parseDateField(dateStr, "last_added", "source", "stats")
	}
	if dateStr, ok := stats.LastModified.(string); ok && dateStr != "" {
		s.LastModified = parseDateField(dateStr, "last_modified", "source", "stats")
	}

	return s, nil
}

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

// facets keeps the SQL ordering. The three list queries differ only in their
// generated row type, which the type parameter erases.
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
