package index

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Search returns the books matching q, using SQL-level filtering for all fields
// including title (LIKE). Within each field values are OR'd; across fields they're
// AND'd. Empty fields are ignored.
func (idx *Index) Search(q model.Query) ([]*model.Book, error) {
	bq := &bookQuery{order: "b.sort_title"}

	addIn(bq, "b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name IN (%s))", q.Authors)
	addIn(bq, "b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name IN (%s))", q.Tags)
	addIn(bq, "b.series_id IN (SELECT id FROM series WHERE name IN (%s))", q.Series)
	addIn(bq, "b.status IN (%s)", q.Status)
	addIn(bq, "b.id IN (%s)", q.IDs)

	if len(q.Titles) > 0 {
		clauses := make([]string, len(q.Titles))
		args := make([]any, len(q.Titles))
		for i, t := range q.Titles {
			clauses[i] = "b.title LIKE ? ESCAPE '\\'"
			args[i] = "%" + escapeSQLLike(t) + "%"
		}
		bq.addCondition(true, "("+strings.Join(clauses, " OR ")+")", args...)
	}

	return idx.queryBooks(bq)
}

// escapeSQLLike escapes the special LIKE characters % and _ in s.
func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
