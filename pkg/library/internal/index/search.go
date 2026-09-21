package index

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

func (idx *Index) Search(q Query) ([]*book.Book, error) {
	bq := &bookQuery{order: orderClause(q.Order), limit: q.Limit}

	// Either column, so a sort-name search finds the book filed under its display name.
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

// Every ordering here is total, so a Limit cannot slice an arbitrary subset of
// tied rows. The date orders break the tie on id descending, since they are
// RFC3339 to the second and two books added in one second still read newest
// first. Rating and pubdate break it alphabetically.
//
// An unrecognised Order sorts by title rather than failing.
func orderClause(o Order) string {
	// sort_title is NULL for most books. Its only source is the EPUB 3 file-as
	// refine, EPUB 2 has no equivalent, and editing a title clears it. Ordering
	// on the bare column would put all of those in one NULL tie and list them
	// by id.
	const byTitle = "COALESCE(b.sort_title, b.title), b.id"
	switch o {
	case OrderDateAdded:
		return "b.date_added DESC, b.id DESC"
	case OrderDateModified:
		return "b.date_modified DESC, b.id DESC"
	case OrderRating:
		return "b.rating DESC, " + byTitle
	case OrderPubdate:
		return "b.pubdate DESC, " + byTitle
	default:
		return byTitle
	}
}

func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}

// Exists is the ingest duplicate rule: the same title and the same set of
// author display names. BookExists in queries.sql does the set comparison,
// which requires names to be distinct.
func (idx *Index) Exists(title string, names []string) (bool, error) {
	return idx.queries.BookExists(idx.ctx, dbsqlc.BookExistsParams{
		Title:       title,
		AuthorCount: int64(len(names)),
		Names:       names,
	})
}
