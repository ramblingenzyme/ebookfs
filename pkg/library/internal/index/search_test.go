package index

import (
	"slices"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Every order, plus the tiebreakers that keep a Limit from slicing an
// arbitrary subset of tied rows.
func TestSearchOrders(t *testing.T) {
	idx := openTestIndex(t)

	mid := newBook(1, "Bravo")
	mid.Meta.Rating = 3
	mid.Pubdate = "2001-01-01"
	top := newBook(2, "Alpha")
	top.Meta.Rating = 5
	top.Pubdate = "2020-01-01"
	// Rating 0 and no pubdate, so these two tie under every order but the
	// title one. Their titles run against their ids, so a missing tiebreak
	// shows up as id order.
	tied := newBook(3, "Delta")
	untied := newBook(4, "Charlie")

	// Leave sort_title unset on some books, so the title ordering has to fall
	// back to the title rather than lump them into one NULL tie.
	mid.SortTitle = "Bravo"
	top.SortTitle = "Alpha"
	// 1 and 2 share a second, which date_added (RFC3339) cannot split, so the
	// id DESC tiebreak orders them. 4 is the oldest but has the highest id, so
	// an order by id alone puts it first.
	added := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	mid.Meta.DateAdded = added
	top.Meta.DateAdded = added
	tied.Meta.DateAdded = added.Add(-time.Hour)
	untied.Meta.DateAdded = added.Add(-2 * time.Hour)
	for _, b := range []*book.Book{mid, top, tied, untied} {
		storeInIndex(t, idx, b)
	}

	for _, tc := range []struct {
		name  string
		order Order
		limit int
		want  []int64
	}{
		// 2 "Alpha" and 1 "Bravo" have sort titles; 4 "Charlie" and 3 "Delta"
		// do not and sort by their titles rather than ahead of everything.
		{"sort title", OrderSortTitle, 0, []int64{2, 1, 4, 3}},
		{"rating", OrderRating, 0, []int64{2, 1, 4, 3}}, // 5, 3, then the 0s by title
		{"pubdate", OrderPubdate, 0, []int64{2, 1, 4, 3}},
		{"recently added", OrderDateAdded, 0, []int64{2, 1, 3, 4}},
		{"unknown order falls back to title", Order(99), 0, []int64{2, 1, 4, 3}},
		{"a limit cuts after the tiebreak", OrderRating, 3, []int64{2, 1, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := idx.Search(Query{Order: tc.order, Limit: tc.limit})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, len(got))
			for i, b := range got {
				ids[i] = b.Meta.ID
			}
			if !slices.Equal(ids, tc.want) {
				t.Errorf("ids = %v, want %v", ids, tc.want)
			}
		})
	}
}

// Exists is set equality, not overlap: order is irrelevant, and neither a
// subset nor a superset of a book's authors matches. It is the ingest
// duplicate rule, so a false positive silently refuses a real book and a false
// negative files the same book twice.
func TestExists(t *testing.T) {
	idx := openTestIndex(t)

	solo := makeTestBook(1, "Foundation", []string{"Isaac Asimov"}, "", book.StatusUnread)
	duo := makeTestBook(2, "Good Omens", []string{"Neil Gaiman", "Terry Pratchett"}, "", book.StatusUnread)
	if err := idx.Rebuild(bookPaths(solo, duo), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	hits := []struct {
		title string
		names []string
	}{
		{"Foundation", []string{"Isaac Asimov"}},
		{"Good Omens", []string{"Neil Gaiman", "Terry Pratchett"}},
		{"Good Omens", []string{"Terry Pratchett", "Neil Gaiman"}}, // the order the rule exists for
	}
	for _, tc := range hits {
		ok, err := idx.Exists(tc.title, tc.names)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("Exists(%q, %q) = false, want true", tc.title, tc.names)
		}
	}

	misses := []struct {
		why   string
		title string
		names []string
	}{
		{"subset of the authors", "Good Omens", []string{"Neil Gaiman"}},
		{"superset of the authors", "Foundation", []string{"Isaac Asimov", "Neil Gaiman"}},
		{"no authors at all", "Foundation", nil},
		{"wrong author", "Foundation", []string{"Frank Herbert"}},
		{"title is exact, not a substring", "found", []string{"Isaac Asimov"}},
		{"right authors, other title", "Dune", []string{"Isaac Asimov"}},
	}
	for _, tc := range misses {
		ok, err := idx.Exists(tc.title, tc.names)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("Exists(%q, %q) = true, want false (%s)", tc.title, tc.names, tc.why)
		}
	}
}
