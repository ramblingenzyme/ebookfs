package index

import (
	"fmt"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// placeholders returns n SQL parameter placeholders ("?") joined by commas.
func placeholders(n int) string {
	if n == 0 {
		return ""
	}
	// strings.Repeat("?,", n) yields trailing comma; slice it off.
	s := strings.Repeat("?,", n)
	return s[:len(s)-1]
}

// Search returns the books matching q, using SQL-level filtering for all fields
// including title (LIKE). Within each field values are OR'd; across fields they're
// AND'd. Empty fields are ignored.
func (idx *Index) Search(q model.Query) ([]*model.Book, error) {
	bq := &bookQuery{order: "b.sort_title"}

	if len(q.Authors) > 0 {
		args := make([]any, len(q.Authors))
		for i, a := range q.Authors {
			args[i] = a
		}
		bq.addCondition(true, fmt.Sprintf(
			"b.id IN (SELECT ba.book_id FROM book_authors ba JOIN authors a ON a.id = ba.author_id WHERE a.name IN (%s))",
			placeholders(len(q.Authors))), args...)
	}

	if len(q.Tags) > 0 {
		args := make([]any, len(q.Tags))
		for i, t := range q.Tags {
			args[i] = t
		}
		bq.addCondition(true, fmt.Sprintf(
			"b.id IN (SELECT bt.book_id FROM book_tags bt JOIN tags t ON t.id = bt.tag_id WHERE t.name IN (%s))",
			placeholders(len(q.Tags))), args...)
	}

	if len(q.Series) > 0 {
		args := make([]any, len(q.Series))
		for i, s := range q.Series {
			args[i] = s
		}
		bq.addCondition(true, fmt.Sprintf(
			"b.series_id IN (SELECT id FROM series WHERE name IN (%s))",
			placeholders(len(q.Series))), args...)
	}

	if len(q.Status) > 0 {
		args := make([]any, len(q.Status))
		for i, s := range q.Status {
			args[i] = s
		}
		bq.addCondition(true, "b.status IN ("+placeholders(len(q.Status))+")", args...)
	}

	if len(q.IDs) > 0 {
		args := make([]any, len(q.IDs))
		for i, id := range q.IDs {
			args[i] = id
		}
		bq.addCondition(true, "b.id IN ("+placeholders(len(q.IDs))+")", args...)
	}

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
