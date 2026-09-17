package textfmt

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library"
)

// ParseQuery parses a Plan 9 clone-style query string into a library.Query. It
// backs both the search view's clone-file queries and ctl's id-spec argument,
// so the two surfaces share one syntax. The grammar is:
//
//	term1+term2+...
//
// where each term is prefix:value. Supported prefixes: author, tag, series,
// status, id, title. Values sharing a prefix are OR'd within the field;
// different prefixes are AND'd across fields. author matches an author's
// display name or sort name, as Index.Search does.
func ParseQuery(query string) (library.Query, error) {
	parts := strings.Split(query, "+")
	var q library.Query
	for _, part := range parts {
		split := strings.SplitN(part, ":", 2)
		if len(split) != 2 { // 1 string, i.e. no ":"
			return q, fmt.Errorf("invalid term %q: want prefix:value", part)
		}
		prefix, val := split[0], split[1]
		switch prefix {
		case "author":
			q.Authors = append(q.Authors, val)
		case "tag":
			q.Tags = append(q.Tags, val)
		case "series":
			q.Series = append(q.Series, val)
		case "status":
			q.Status = append(q.Status, val)
		case "id":
			id, err := strconv.ParseInt(val, 10, 64)
			if err != nil {
				return q, fmt.Errorf("invalid id %q in term %q", val, part)
			}
			q.IDs = append(q.IDs, id)
		case "title":
			q.Titles = append(q.Titles, val)
		default:
			return q, fmt.Errorf("unknown prefix %q in term %q", prefix, part)
		}
	}
	return q, nil
}
