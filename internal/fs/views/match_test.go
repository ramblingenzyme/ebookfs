package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// makeMatchesFn is the single authority a handle uses for both its resync and
// its live updates, so every field needs a matching and a non-matching case, and
// the AND across fields needs one where a single field fails while the rest hold.
func TestMakeMatchesFn(t *testing.T) {
	base := func() *library.Book {
		b := makeBook(7, "Foundation and Empire", "Isaac Asimov", "Ray Bradbury")
		b.Meta.Tags = []string{"sci-fi", "classic"}
		b.Meta.Status = "unread"
		b.Series = &library.Series{Name: "Foundation", Index: "2"}
		return wrapBook(b)
	}
	standalone := func() *library.Book {
		b := makeBook(7, "Foundation and Empire", "Isaac Asimov", "Ray Bradbury")
		b.Meta.Tags = []string{"sci-fi", "classic"}
		b.Meta.Status = "unread"
		b.Series = nil
		return wrapBook(b)
	}

	tests := []struct {
		name  string
		query library.Query
		book  func() *library.Book
		want  bool
	}{
		{"empty query matches anything", library.Query{}, base, true},

		{"author matches", library.Query{Authors: []string{"Isaac Asimov"}}, base, true},
		// Any one author of a multi-author book is enough.
		{"co-author matches", library.Query{Authors: []string{"Ray Bradbury"}}, base, true},
		{"author does not match", library.Query{Authors: []string{"J.R.R. Tolkien"}}, base, false},
		// Authors compare exactly; unlike titles, they are not a substring field.
		{"author is not a substring match", library.Query{Authors: []string{"Asimov"}}, base, false},

		{"tag matches", library.Query{Tags: []string{"sci-fi"}}, base, true},
		{"tag matches any of several", library.Query{Tags: []string{"fantasy", "classic"}}, base, true},
		{"tag does not match", library.Query{Tags: []string{"fantasy"}}, base, false},

		{"series matches", library.Query{Series: []string{"Foundation"}}, base, true},
		{"series does not match", library.Query{Series: []string{"Dune"}}, base, false},
		// A book in no series can never satisfy a series filter, and must not
		// panic on the nil.
		{"series filter rejects a standalone book", library.Query{Series: []string{"Foundation"}}, standalone, false},

		{"status matches", library.Query{Status: []string{"unread"}}, base, true},
		{"status does not match", library.Query{Status: []string{"read"}}, base, false},

		{"id matches", library.Query{IDs: []int64{7}}, base, true},
		{"id does not match", library.Query{IDs: []int64{8}}, base, false},

		// Titles are the one substring field, and case-insensitive on both sides.
		{"title substring matches", library.Query{Titles: []string{"and Empire"}}, base, true},
		{"title match ignores case", library.Query{Titles: []string{"FOUNDATION"}}, base, true},
		{"title does not match", library.Query{Titles: []string{"Hobbit"}}, base, false},
		// ExactTitles is ctl's setting, but the predicate honours it so this
		// path and Index.Search stay one semantics.
		{"exact title matches", library.Query{Titles: []string{"Foundation and Empire"}, ExactTitles: true}, base, true},
		{"exact title rejects a substring", library.Query{Titles: []string{"and Empire"}, ExactTitles: true}, base, false},

		// Across fields every populated one must hold...
		{
			"every field matching",
			library.Query{
				Authors: []string{"Isaac Asimov"},
				Tags:    []string{"sci-fi"},
				Series:  []string{"Foundation"},
				Status:  []string{"unread"},
				IDs:     []int64{7},
				Titles:  []string{"empire"},
			},
			base, true,
		},
		// ...so one failing field rejects the book even when the others match.
		{"one field of several fails", library.Query{Tags: []string{"sci-fi"}, Status: []string{"read"}}, base, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := makeMatchesFn(tc.query)(tc.book()); got != tc.want {
				t.Errorf("matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// The value is snapshotted at open like every other file in the tree, so a fid
// reports the query that was current when it opened rather than tracking later
// requeries.
