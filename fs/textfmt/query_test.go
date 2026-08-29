package textfmt

import (
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"
)

// TestParseQuery covers the query language itself: every supported
// prefix, the OR-within-a-field / AND-across-fields shape the syntax promises,
// and each way a term can be rejected.
func TestParseQuery(t *testing.T) {
	tests := []struct {
		name    string
		query   string
		want    library.Query
		wantErr bool
	}{
		{"author", "author:Isaac Asimov", library.Query{Authors: []string{"Isaac Asimov"}}, false},
		{"tag", "tag:sci-fi", library.Query{Tags: []string{"sci-fi"}}, false},
		{"series", "series:Foundation", library.Query{Series: []string{"Foundation"}}, false},
		{"status", "status:unread", library.Query{Status: []string{"unread"}}, false},
		{"id", "id:42", library.Query{IDs: []int64{42}}, false},
		{"title", "title:Dune", library.Query{Titles: []string{"Dune"}}, false},
		// A repeated prefix accumulates into one field, which the matcher ORs...
		{"repeated prefix", "tag:sci-fi+tag:fantasy", library.Query{Tags: []string{"sci-fi", "fantasy"}}, false},
		// ...while distinct prefixes populate distinct fields, which it ANDs.
		{"distinct prefixes", "tag:sci-fi+status:unread", library.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}}, false},
		// The term splits on its first colon only, so a value may contain more.
		{"colon in value", "title:Dune: Part Two", library.Query{Titles: []string{"Dune: Part Two"}}, false},
		// An empty value is a term, not a parse error — it reaches the matcher
		// and simply matches nothing.
		{"empty value", "tag:", library.Query{Tags: []string{""}}, false},
		{"no colon", "sci-fi", library.Query{}, true},
		{"unknown prefix", "publisher:Tor", library.Query{}, true},
		{"non-numeric id", "id:abc", library.Query{}, true},
		// Reached only if a caller skips its own empty check — searchCtlFile.Close
		// drops empty writes and parseSelection rejects an empty id-spec.
		{"empty query", "", library.Query{}, true},
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
