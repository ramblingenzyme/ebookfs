package index

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Search returns the books matching q, using SQL-level filtering for all fields
// including title (LIKE, or "=" when q.ExactTitles). Within each field values
// are OR'd; across fields they're AND'd. Empty fields are ignored, so Query{} returns every book.
func (idx *Index) Search(q model.Query) ([]*book.Book, error) {
	bq := &bookQuery{order: orderClause(q.Order), limit: q.Limit}

	// Either column: a sort-name search must find the book filed under its display name.
	addIn(bq, "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name IN (%s) OR a.sort_name IN (%s))", q.Authors)
	addIn(bq, "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name IN (%s))", q.Tags)
	addIn(bq, "b.series_id IN (SELECT id FROM series WHERE name IN (%s))", q.Series)
	addIn(bq, "b.status IN (%s)", q.Status)
	addIn(bq, "b.id IN (%s)", q.IDs)

	if q.ExactTitles {
		addIn(bq, "b.title IN (%s)", q.Titles)
	} else if len(q.Titles) > 0 {
		clauses := make([]string, len(q.Titles))
		args := make([]any, len(q.Titles))
		for i, t := range q.Titles {
			clauses[i] = "b.title LIKE ? ESCAPE '\\'"
			args[i] = "%" + escapeSQLLike(t) + "%"
		}
		bq.addCondition("("+strings.Join(clauses, " OR ")+")", args...)
	}

	return idx.queryBooks(bq)
}

// orderClause maps an Order to its ORDER BY. Every ordering is total, so a
// Limit cannot slice an arbitrary subset of tied rows: id breaks the last tie,
// since sort_title is nullable and not unique. Ties resolve the way the primary
// column intends — the date orders fall back to id descending, because the
// dates are RFC3339 to the second and two books added in the same second should
// still read newest first, while rating and pubdate fall back alphabetically.
// An unrecognised Order sorts by title rather than failing, since ordering is
// presentation and a bad one should not turn a search into an error.
func orderClause(o model.Order) string {
	// sort_title is NULL for most books: its only source is the EPUB 3 file-as
	// refine, EPUB 2 has no equivalent, and editing a title clears it. Ordering
	// on the bare column would put every one of those in a single NULL tie and
	// list them by id, so fall back to the title itself.
	const byTitle = "COALESCE(b.sort_title, b.title), b.id"
	switch o {
	case model.OrderDateAdded:
		return "b.date_added DESC, b.id DESC"
	case model.OrderDateModified:
		return "b.date_modified DESC, b.id DESC"
	case model.OrderRating:
		return "b.rating DESC, " + byTitle
	case model.OrderPubdate:
		return "b.pubdate DESC, " + byTitle
	default:
		return byTitle
	}
}

// escapeSQLLike escapes the special LIKE characters % and _ in s.
func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// Exists reports whether the index holds a book with exactly this title and
// exactly this set of author display names — the ingest duplicate rule. The set
// comparison is in SQL: the book must have len(names) authors and none outside
// names. See BookExists in queries.sql. names must be distinct.
func (idx *Index) Exists(title string, names []string) (bool, error) {
	return idx.queries.BookExists(idx.ctx, dbsqlc.BookExistsParams{
		Title:       title,
		AuthorCount: int64(len(names)),
		Names:       names,
	})
}
