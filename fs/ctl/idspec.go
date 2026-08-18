package ctl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// parseSelection resolves a ctl id-spec to a model.Query addressing the
// selected books. It is the id shorthands layered over textfmt.ParseQuery:
//
//	"*"      — all books
//	42       — single book
//	1,2,3    — comma-separated list
//
// A spec containing ":" is a query in the search view's syntax instead
// ("tag:sci-fi+status:unread"), so both surfaces take the same language.
//
// "*" resolves to an empty Query{}, which Search treats as "every book",
// rather than enumerating every id and binding one SQL variable per book,
// which would overflow SQLite's variable limit on a large library.
func parseSelection(spec string) (model.Query, error) {
	spec = strings.TrimSpace(spec)

	switch {
	case spec == "":
		return model.Query{}, fmt.Errorf("empty id-spec")
	case spec == "*":
		return model.Query{}, nil
	case strings.Contains(spec, ":"):
		q, err := textfmt.ParseQuery(spec)
		// A ctl selection feeds a mutating command, so title: must not reach
		// past the book the operator named the way the search view's substring
		// match would.
		q.ExactTitles = true
		return q, err
	default:
		return parseIdList(spec)
	}
}

func parseIdList(spec string) (model.Query, error) {
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
