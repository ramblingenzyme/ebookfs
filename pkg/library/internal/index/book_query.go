package index

import (
	"fmt"
	"slices"
	"strings"
)

type bookQuery struct {
	where []string
	args  []any
	order string
	limit int
}

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

func (q *bookQuery) addCondition(expr string, args ...any) {
	q.where = append(q.where, expr)
	q.args = append(q.args, args...)
}

func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// addIn binds vals once per %s verb in expr, so one value set can be tested
// against two columns (name OR sort_name) without the caller assembling the
// arguments twice. Empty vals is a no-op, so callers need no length check.
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
