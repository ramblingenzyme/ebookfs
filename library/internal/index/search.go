package index

import (
	"fmt"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Search returns the books matching q, using SQL-level filtering for all fields
// including title (LIKE). Within each field values are OR'd; across fields they're
// AND'd. Empty fields are ignored.
func (idx *Index) Search(q model.Query) ([]*model.Book, error) {
	var where []string
	var args []any

	if len(q.Authors) > 0 {
		placeholders := make([]string, len(q.Authors))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		for _, a := range q.Authors {
			args = append(args, a)
		}
		where = append(where, fmt.Sprintf(
			"b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name IN (%s))",
			strings.Join(placeholders, ",")))
	}

	if len(q.Tags) > 0 {
		placeholders := make([]string, len(q.Tags))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		for _, t := range q.Tags {
			args = append(args, t)
		}
		where = append(where, fmt.Sprintf(
			"b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name IN (%s))",
			strings.Join(placeholders, ",")))
	}

	if len(q.Series) > 0 {
		placeholders := make([]string, len(q.Series))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		for _, s := range q.Series {
			args = append(args, s)
		}
		where = append(where, fmt.Sprintf(
			"b.series_id IN (SELECT id FROM series WHERE name IN (%s))",
			strings.Join(placeholders, ",")))
	}

	if len(q.Status) > 0 {
		placeholders := make([]string, len(q.Status))
		for i := range placeholders {
			placeholders[i] = "?"
		}
		for _, s := range q.Status {
			args = append(args, s)
		}
		where = append(where, "b.status IN ("+strings.Join(placeholders, ",")+")")
	}

	if len(q.IDs) > 0 {
		placeholders := make([]string, len(q.IDs))
		for i, id := range q.IDs {
			placeholders[i] = "?"
			args = append(args, id)
		}
		where = append(where, "b.id IN ("+strings.Join(placeholders, ",")+")")
	}

	if len(q.Titles) > 0 {
		clauses := make([]string, len(q.Titles))
		for i, t := range q.Titles {
			clauses[i] = "b.title LIKE ? ESCAPE '\\'"
			args = append(args, "%"+escapeSQLLike(t)+"%")
		}
		where = append(where, "("+strings.Join(clauses, " OR ")+")")
	}

	whereClause := strings.Join(where, " AND ")
	return idx.queryBooks(whereClause, args, "b.sort_title", 0)
}

// escapeSQLLike escapes the special LIKE characters % and _ in s.
func escapeSQLLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
