package textfmt

import (
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// TestParseQuery covers the query language itself: every supported
// prefix, the OR-within-a-field / AND-across-fields shape the syntax promises,
// and each way a term can be rejected.
func TestParseQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    model.Query
		wantErr bool
	}{
		{"author", "author:Isaac Asimov", model.Query{Authors: []string{"Isaac Asimov"}}, false},
		{"tag", "tag:sci-fi", model.Query{Tags: []string{"sci-fi"}}, false},
		{"series", "series:Foundation", model.Query{Series: []string{"Foundation"}}, false},
		{"status", "status:unread", model.Query{Status: []string{"unread"}}, false},
		{"id", "id:42", model.Query{IDs: []int64{42}}, false},
		{"title", "title:Dune", model.Query{Titles: []string{"Dune"}}, false},
		// A repeated prefix accumulates into one field, which the matcher ORs...
		{"repeated prefix", "tag:sci-fi+tag:fantasy", model.Query{Tags: []string{"sci-fi", "fantasy"}}, false},
		// ...while distinct prefixes populate distinct fields, which it ANDs.
		{"distinct prefixes", "tag:sci-fi+status:unread", model.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}}, false},
		// The term splits on its first colon only, so a value may contain more.
		{"colon in value", "title:Dune: Part Two", model.Query{Titles: []string{"Dune: Part Two"}}, false},
		// An empty value is a term, not a parse error — it reaches the matcher
		// and simply matches nothing.
		{"empty value", "tag:", model.Query{Tags: []string{""}}, false},
		{"no colon", "sci-fi", model.Query{}, true},
		{"unknown prefix", "publisher:Tor", model.Query{}, true},
		{"non-numeric id", "id:abc", model.Query{}, true},
		// Reached only if a caller skips its own empty check — searchCtlFile.Close
		// drops empty writes and parseSelection rejects an empty id-spec.
		{"empty query", "", model.Query{}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseQuery(tc.query)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseQuery(%q) = %+v, want an error", tc.query, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseQuery(%q): %v", tc.query, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseQuery(%q) = %+v, want %+v", tc.query, got, tc.want)
			}
		})
	}
}
