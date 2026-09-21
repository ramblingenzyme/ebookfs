package ctl

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// parseSelection resolves a ctl id-spec to a library.Query: the id shorthands
// layered over textfmt.ParseQuery, so both surfaces take one language.
//
// "*" resolves to an empty Query{}, which Search reads as every book, rather
// than binding one SQL variable per id and overflowing SQLite's limit.
func parseSelection(spec string) (library.Query, error) {
	spec = strings.TrimSpace(spec)

	switch {
	case spec == "":
		return library.Query{}, fmt.Errorf("empty id-spec")
	case spec == "*":
		return library.Query{}, nil
	case strings.Contains(spec, ":"):
		q, err := textfmt.ParseQuery(spec)
		if err != nil {
			return q, err
		}
		// All parts of the spec are exact to prevent more books being
		// updated than expected.
		q.ExactTitles = true
		return q, err
	default:
		return parseIdList(spec)
	}
}

func parseIdList(spec string) (library.Query, error) {
	parts := strings.Split(spec, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return library.Query{}, fmt.Errorf("invalid id %q in spec %q", p, spec)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return library.Query{}, fmt.Errorf("no valid ids in spec %q", spec)
	}
	return library.Query{IDs: ids}, nil
}
