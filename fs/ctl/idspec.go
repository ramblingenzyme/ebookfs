package ctl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// parseSelection resolves an id-spec string to a model.Query addressing the
// selected books.
//
// Supported formats:
//
//   - — all books
//     42       — single book
//     1,2,3    — comma-separated list
//
// "*" resolves to an empty Query{}, which Search treats as "every book",
// rather than enumerating every id and binding one SQL variable per book,
// which would overflow SQLite's variable limit on a large library.
//
// TODO: support query syntax (tag:sci-fi, status:unread, etc.).
func parseSelection(spec string) (model.Query, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return model.Query{}, fmt.Errorf("empty id-spec")
	}
	if spec == "*" {
		return model.Query{}, nil
	}

	parts := strings.Split(spec, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return model.Query{}, fmt.Errorf("invalid id %q in spec %q", p, spec)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return model.Query{}, fmt.Errorf("no valid ids in spec %q", spec)
	}
	return model.Query{IDs: ids}, nil
}
