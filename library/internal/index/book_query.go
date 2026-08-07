package index

import "strings"

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

// addCondition appends a WHERE clause if cond is true.
func (q *bookQuery) addCondition(cond bool, expr string, args ...any) {
	if cond {
		q.where = append(q.where, expr)
		q.args = append(q.args, args...)
	}
}
