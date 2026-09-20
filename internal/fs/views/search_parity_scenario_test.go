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

// parityBooks opens a library holding one book per title/author pair and
// returns it with every book in it, which is the set the Go matcher filters.
func parityBooks(t *testing.T, books [][2]string) (*library.Library, []*library.Book) {
	t.Helper()
	lib, err := library.Open(library.Config(util.TestConfig(t)))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })

	for _, b := range books {
		h, err := lib.CreateIngest()
		if err != nil {
			t.Fatalf("CreateIngest: %v", err)
		}
		if _, err := h.WriteAt(util.BuildTestEpub(t, b[0], b[1]), 0); err != nil {
			t.Fatalf("WriteAt: %v", err)
		}
		if _, err := h.Ingest(); err != nil {
			t.Fatalf("Ingest %q: %v", b[0], err)
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

func TestSearchMatcherAgreesWithSQL(t *testing.T) {
	lib, all := parityBooks(t, [][2]string{
		{"Findable", "Alice"},
		{"Ünderland", "Bob"},
		{"STRAÜSS", "Carol"},
		{"Plain Book", "Alice"},
	})

	for _, tc := range []struct {
		name string
		q    library.Query
		// skip names the known divergence a case exercises. SQLite's LIKE folds case
		// for ASCII only; makeMatchesFn uses strings.ToLower, which folds the whole
		// of Unicode. Fixing it is a real choice, teaching SQLite a Unicode-aware
		// comparison or stopping the Go side folding past ASCII, and the two give
		// different behaviour. Drop the skip to see it fail.
		skip string
	}{
		{name: "everything", q: library.Query{}},
		{name: "title substring", q: library.Query{Titles: []string{"ind"}}},
		{name: "title lowercased ascii", q: library.Query{Titles: []string{"findable"}}},
		{name: "title exact", q: library.Query{Titles: []string{"Findable"}, ExactTitles: true}},
		{name: "title exact wrong case", q: library.Query{Titles: []string{"findable"}, ExactTitles: true}},
		{name: "author", q: library.Query{Authors: []string{"Alice"}}},
		{name: "author and title", q: library.Query{Authors: []string{"Alice"}, Titles: []string{"Plain"}}},
		{name: "no match", q: library.Query{Titles: []string{"nothing here"}}},
		{
			name: "title lowercased non-ascii",
			q:    library.Query{Titles: []string{"ünderland"}},
			skip: "SQL misses Ünderland, Go finds it",
		},
		{
			name: "title uppercased non-ascii",
			q:    library.Query{Titles: []string{"straüss"}},
			skip: "SQL misses STRAÜSS, Go finds it",
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
			if sql, gos := titlesOf(fromSQL), titlesOf(fromGo); !slices.Equal(sql, gos) {
				t.Errorf("matchers disagree for %+v\n  library.Search (SQL): %v\n  makeMatchesFn (Go):   %v", tc.q, sql, gos)
			}
		})
	}
}
