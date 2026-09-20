package index

import (
	"fmt"
	"slices"
	"strings"
)

// bookQuery represents a parameterized book query. WHERE clauses are AND'd together.
type bookQuery struct {
	where []string
	args  []any
	order string
	limit int
}

// sql builds the SELECT statement from the query components.
func (q *bookQuery) sql() (string, []any) {
	qry := `
		SELECT b.id, b.title, b.sort_title, COALESCE(b.pubdate, ''), b.description, b.language,
			b.epub_path, b.cover_path,
			b.status, b.rating, b.date_added, b.date_modified,
			s.id, s.name, b.series_index,
			b.opf_size, b.cover_size, b.epub_size
		FROM books b
		LEFT JOIN series s ON s.id = b.series_id`
	if len(q.where) > 0 {
		qry += " WHERE " + strings.Join(q.where, " AND ")
	}
	qry += " ORDER BY " + q.order
	if q.limit > 0 {
		qry += " LIMIT ?"
		q.args = append(q.args, q.limit)
	}
	return qry, q.args
}

// addCondition appends a WHERE clause and its bound arguments.
func (q *bookQuery) addCondition(expr string, args ...any) {
	q.where = append(q.where, expr)
	q.args = append(q.args, args...)
}

// placeholders returns n SQL parameter placeholders ("?") joined by commas.
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// addIn appends an IN-list condition to bq. Each %s verb in expr takes the
// placeholder list, and vals are bound once per verb — so one value set can be
// tested against two columns (name OR sort_name) without the caller assembling
// the arguments twice. Empty vals is a no-op, so callers need no length check.
func addIn[T any](bq *bookQuery, expr string, vals []T) {
	if len(vals) == 0 {
		return
	}
	n := strings.Count(expr, "%s")
	lists := slices.Repeat([]any{placeholders(len(vals))}, n)
	args := make([]any, 0, len(vals)*n)
	for _, v := range slices.Repeat(vals, n) {
		args = append(args, v)
	}
	bq.addCondition(fmt.Sprintf(expr, lists...), args...)
}
