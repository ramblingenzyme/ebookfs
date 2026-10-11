// One query text, two matchers. ctl parses it and hands it to library.Search,
// which filters in SQL. The search view parses the same text and hands it to
// makeMatchesFn, which filters in Go over the registry's books. Nothing in
// either package's types says the two must agree, and the consequence of
// disagreeing is not symmetric: a book the view shows but ctl misses is a bulk
// edit silently skipping it.
//
// Spans views and library, so it pairs with neither source file.

package views

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type parityBook struct {
	title   string
	authors []string
	edits   library.Edits
}

func TestSearchMatcherAgreesWithSQL(t *testing.T) {
	books := []parityBook{
		{"Findable", []string{"Alice"}, library.Edits{Tags: &[]string{"sf"}, Status: new("reading"), Series: new("Saga"), SeriesIndex: new("1")}},
		{"Plain Book", []string{"Alice"}, library.Edits{}},
		{"Ünderland", []string{"Bob"}, library.Edits{Tags: &[]string{"fantasy"}, Status: new("read"), Series: new("Saga"), SeriesIndex: new("2"), Authors: &[]library.Author{{Name: "Bob", SortName: "Bobson, Bob"}}}},
		{"STRAÜSS", []string{"Carol"}, library.Edits{}},
		{"Good Omens", []string{"Neil Gaiman", "Terry Pratchett"}, library.Edits{}},
		{"100% Wool", []string{"Dave"}, library.Edits{}},
		{"1000 Ships", []string{"Dave"}, library.Edits{}},
		{"x_y Files", []string{"Erin"}, library.Edits{}},
		{"xzy Files", []string{"Erin"}, library.Edits{}},
	}

	lib, all := parityBooks(t, books)

	for _, tc := range []struct {
		name string
		q    library.Query
		want []string
		skip string
	}{
		{"everything", library.Query{}, []string{"100% Wool", "1000 Ships", "Findable", "Good Omens", "Plain Book", "STRAÜSS", "x_y Files", "xzy Files", "Ünderland"}, ""},
		{"title substring", library.Query{Titles: []string{"ind"}}, []string{"Findable"}, ""},
		{"title lowercased ascii", library.Query{Titles: []string{"findable"}}, []string{"Findable"}, ""},
		{"title exact", library.Query{Titles: []string{"Findable"}, ExactTitles: true}, []string{"Findable"}, ""},
		{"title exact wrong case", library.Query{Titles: []string{"findable"}, ExactTitles: true}, nil, ""},
		{"author", library.Query{Authors: []string{"Alice"}}, []string{"Findable", "Plain Book"}, ""},
		{"co-author", library.Query{Authors: []string{"Terry Pratchett"}}, []string{"Good Omens"}, ""},
		{"author by sort name", library.Query{Authors: []string{"Bobson, Bob"}}, []string{"Ünderland"}, ""},
		{"author is not a substring", library.Query{Authors: []string{"Ali"}}, nil, ""},
		{"author and title", library.Query{Authors: []string{"Alice"}, Titles: []string{"Plain"}}, []string{"Plain Book"}, ""},
		{"tag", library.Query{Tags: []string{"sf"}}, []string{"Findable"}, ""},
		{"tags OR", library.Query{Tags: []string{"sf", "fantasy"}}, []string{"Findable", "Ünderland"}, ""},
		{"series", library.Query{Series: []string{"Saga"}}, []string{"Findable", "Ünderland"}, ""},
		{"status", library.Query{Status: []string{"reading"}}, []string{"Findable"}, ""},
		{"ids", library.Query{IDs: []int64{1, 3}}, []string{"Findable", "Ünderland"}, ""},
		{"every field", library.Query{Authors: []string{"Alice"}, Tags: []string{"sf"}, Series: []string{"Saga"}, Status: []string{"reading"}, IDs: []int64{1}, Titles: []string{"find"}}, []string{"Findable"}, ""},
		{"one field fails", library.Query{Tags: []string{"sf"}, Status: []string{"read"}}, nil, ""},
		{"literal percent", library.Query{Titles: []string{"100%"}}, []string{"100% Wool"}, ""},
		{"literal underscore", library.Query{Titles: []string{"x_y"}}, []string{"x_y Files"}, ""},
		{"no match", library.Query{Titles: []string{"nothing here"}}, nil, ""},
		{
			"title lowercased non-ascii",
			library.Query{Titles: []string{"ünderland"}},
			[]string{"Ünderland"},
			"SQL misses Ünderland, Go finds it",
		},
		{
			"title uppercased non-ascii",
			library.Query{Titles: []string{"straüss"}},
			[]string{"STRAÜSS"},
			"SQL misses STRAÜSS, Go finds it",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skip != "" {
				t.Skip("known divergence: " + tc.skip)
			}
			fromSQL, err := lib.Search(tc.q)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			var fromGo []*library.Book
			matches := makeMatchesFn(tc.q)
			for _, b := range all {
				if matches(b) {
					fromGo = append(fromGo, b)
				}
			}
			sqlTitles := titlesOf(fromSQL)
			goTitles := titlesOf(fromGo)
			if !slices.Equal(sqlTitles, tc.want) {
				t.Errorf("SQL: got %v, want %v", sqlTitles, tc.want)
			}
			if !slices.Equal(goTitles, tc.want) {
				t.Errorf("Go: got %v, want %v", goTitles, tc.want)
			}
		})
	}
}

// parityBooks opens a library holding one book per entry and returns it with
// every book in it, which is the set the Go matcher filters.
func parityBooks(t *testing.T, books []parityBook) (*library.Library, []*library.Book) {
	t.Helper()
	lib, err := library.Open(library.Config(util.TestConfig(t)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })

	for i, b := range books {
		h, err := lib.CreateIngest()
		if err != nil {
			t.Fatalf("CreateIngest: %v", err)
		}
		if _, err := h.WriteAt(util.BuildTestEpub(t, b.title, b.authors...), 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		book, err := h.Ingest()
		if err != nil {
			t.Fatalf("Ingest %q: %v", b.title, err)
		}
		if b.edits != (library.Edits{}) {
			if _, err := lib.Edit(book.ID(), b.edits); err != nil {
				t.Fatalf("Edit %d: %v", i+1, err)
			}
		}
	}

	all, err := lib.Search(library.Query{})
	if err != nil {
		t.Fatalf("Search(all): %v", err)
	}
	return lib, all
}

func titlesOf(books []*library.Book) []string {
	out := make([]string, 0, len(books))
	for _, b := range books {
		out = append(out, b.Title())
	}
	slices.Sort(out)
	return out
}
