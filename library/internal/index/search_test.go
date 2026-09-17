package index

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestSearch(t *testing.T) {
	idx := openTestIndex(t)

	b1 := makeTestBook(1, "Foundation", []string{"Isaac Asimov"}, "sci-fi", book.StatusRead)
	b2 := makeTestBook(2, "Dune", []string{"Frank Herbert"}, "sci-fi", book.StatusUnread)
	b3 := makeTestBook(3, "The Hobbit", []string{"J.R.R. Tolkien"}, "fantasy", book.StatusRead)
	if err := idx.Rebuild(bookPaths(b1, b2, b3), nil, 3); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	t.Run("empty query returns all", func(t *testing.T) {
		books, err := idx.Search(Query{})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 3 {
			t.Fatalf("got %d books, want 3", len(books))
		}
	})

	t.Run("single tag", func(t *testing.T) {
		books, err := idx.Search(Query{Tags: []string{"sci-fi"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 2 {
			t.Fatalf("got %d books, want 2", len(books))
		}
	})

	t.Run("multiple tags OR", func(t *testing.T) {
		books, err := idx.Search(Query{Tags: []string{"sci-fi", "fantasy"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 3 {
			t.Fatalf("got %d books, want 3", len(books))
		}
	})

	t.Run("tag AND status", func(t *testing.T) {
		books, err := idx.Search(Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
		if books[0].Title != "Dune" {
			t.Errorf("got %q, want Dune", books[0].Title)
		}
	})

	t.Run("title substring", func(t *testing.T) {
		books, err := idx.Search(Query{Titles: []string{"found"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
		if books[0].Title != "Foundation" {
			t.Errorf("got %q, want Foundation", books[0].Title)
		}
	})

	// ExactTitles turns the substring match into equality, so the same value
	// that found Foundation above finds nothing.
	t.Run("title exact", func(t *testing.T) {
		books, err := idx.Search(Query{Titles: []string{"found"}, ExactTitles: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 0 {
			t.Fatalf("got %d books, want 0", len(books))
		}

		books, err = idx.Search(Query{Titles: []string{"Foundation"}, ExactTitles: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 || books[0].Title != "Foundation" {
			t.Fatalf("got %d books, want Foundation", len(books))
		}
	})

	t.Run("author name", func(t *testing.T) {
		books, err := idx.Search(Query{Authors: []string{"Isaac Asimov"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
	})

	t.Run("no match", func(t *testing.T) {
		books, err := idx.Search(Query{Tags: []string{"nonexistent"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 0 {
			t.Fatalf("got %d books, want 0", len(books))
		}
	})
}

func TestSearchIDs(t *testing.T) {
	idx := openTestIndex(t)

	b1 := makeTestBook(1, "A", nil, "", "")
	b2 := makeTestBook(2, "B", nil, "", "")
	if err := idx.Rebuild(bookPaths(b1, b2), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	books, err := idx.Search(Query{IDs: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("got %d books, want 1", len(books))
	}
	if books[0].Meta.ID != 1 {
		t.Errorf("got id %d, want 1", books[0].Meta.ID)
	}

	books, err = idx.Search(Query{IDs: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("got %d books, want 2", len(books))
	}
}

// The remaining orders, plus the sort-title tiebreaker that keeps a Limit from
// slicing an arbitrary subset of tied rows.
func TestSearchOrders(t *testing.T) {
	idx := openTestIndex(t)

	mid := newBook(1, "Bravo")
	mid.Meta.Rating = 3
	mid.Pubdate = "2001-01-01"
	top := newBook(2, "Alpha")
	top.Meta.Rating = 5
	top.Pubdate = "2020-01-01"
	// Rating 0 and no pubdate, so these two tie under every order but the
	// title one, exercising the tiebreaker.
	tied := newBook(3, "Charlie")
	untied := newBook(4, "Delta")

	// sort_title is NULL unless the epub carried a file-as refine, so leave it
	// unset on some books: the title ordering has to fall back to the title,
	// not lump them into one NULL tie ordered by id.
	mid.SortTitle = "Bravo"
	top.SortTitle = "Alpha"
	for _, b := range []*book.Book{mid, top, tied, untied} {
		storeInIndex(t, idx, b)
	}

	for _, tc := range []struct {
		name  string
		order Order
		want  []int64
	}{
		// 2 "Alpha" and 1 "Bravo" have sort titles; 3 "Charlie" and 4 "Delta"
		// do not and sort by their titles rather than ahead of everything.
		{"sort title", OrderSortTitle, []int64{2, 1, 3, 4}},
		{"rating", OrderRating, []int64{2, 1, 3, 4}}, // 5, 3, then the 0s by title
		{"pubdate", OrderPubdate, []int64{2, 1, 3, 4}},
		{"unknown order falls back to title", Order(99), []int64{2, 1, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := idx.Search(Query{Order: tc.order})
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

func TestQueryAllReturnsAllBooks(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Alpha"))
	storeInIndex(t, idx, newBook(2, "Beta"))
	storeInIndex(t, idx, newBook(3, "Gamma"))

	books, err := idx.Search(Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(books) != 3 {
		t.Fatalf("len = %d, want 3", len(books))
	}
}

func TestQueryByID(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "One"))
	storeInIndex(t, idx, newBook(2, "Two"))

	got, err := idx.Search(Query{IDs: []int64{2}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 2 {
		t.Errorf("ID = %d, want 2", got[0].Meta.ID)
	}
}

func TestQueryByAuthor(t *testing.T) {
	idx := openTestIndex(t)

	bob := book.NewBook(
		book.Bib{Title: "Bob's Book", Authors: []book.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "Bob/Bob's Book (1)/book.epub"},
	)
	aliceBook := book.NewBook(
		book.Bib{Title: "Alice's Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "Alice/Alice's Book (2)/book.epub"},
	)

	storeInIndex(t, idx, bob)
	storeInIndex(t, idx, aliceBook)

	got, err := idx.Search(Query{Authors: []string{"Bob"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Bob's Book" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Bob's Book")
	}
}

// TestQueryByAuthorSortName pins the two-column author match. ctl's
// rename-author documents that <old> matches display name OR sort name, and it
// relies on this query to find the books to rewrite — if only a.name were
// matched, renaming by sort name would silently rewrite nothing.
func TestQueryByAuthorSortName(t *testing.T) {
	idx := openTestIndex(t)

	asimov := book.NewBook(
		book.Bib{Title: "Foundation", Authors: []book.Author{{Name: "Isaac Asimov", SortName: "Asimov, Isaac"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "Isaac Asimov/Foundation (1)/book.epub"},
	)
	other := book.NewBook(
		book.Bib{Title: "Other", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "Alice/Other (2)/book.epub"},
	)
	storeInIndex(t, idx, asimov)
	storeInIndex(t, idx, other)

	for _, name := range []string{"Asimov, Isaac", "Isaac Asimov"} {
		got, err := idx.Search(Query{Authors: []string{name}})
		if err != nil {
			t.Fatalf("Search(%q): %v", name, err)
		}
		if len(got) != 1 || got[0].Title != "Foundation" {
			t.Errorf("Search(%q) = %v, want just Foundation", name, got)
		}
	}
}

func TestQueryByStatus(t *testing.T) {
	idx := openTestIndex(t)

	readBook := book.NewBook(
		book.Bib{Title: "Read Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Status: "read"},
		book.Location{EpubPath: "A/Read Book (1)/book.epub"},
	)
	unreadBook := book.NewBook(
		book.Bib{Title: "Unread Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2, Status: "unread"},
		book.Location{EpubPath: "A/Unread Book (2)/book.epub"},
	)

	storeInIndex(t, idx, readBook)
	storeInIndex(t, idx, unreadBook)

	got, err := idx.Search(Query{Status: []string{"read"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 1 {
		t.Errorf("returned book id = %d, want 1", got[0].Meta.ID)
	}
}

func TestQueryByTag(t *testing.T) {
	idx := openTestIndex(t)

	tagged := book.NewBook(
		book.Bib{Title: "Tagged", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"sci-fi"}},
		book.Location{EpubPath: "A/Tagged (1)/book.epub"},
	)
	untagged := book.NewBook(
		book.Bib{Title: "Plain", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Plain (2)/book.epub"},
	)

	storeInIndex(t, idx, tagged)
	storeInIndex(t, idx, untagged)

	got, err := idx.Search(Query{Tags: []string{"sci-fi"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 1 {
		t.Errorf("returned book id = %d, want 1", got[0].Meta.ID)
	}
}

func TestQueryBySeries(t *testing.T) {
	idx := openTestIndex(t)

	seriesBook := book.NewBook(
		book.Bib{
			Title: "Series Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}},
			Series: &book.SeriesRef{Name: "My Series", Index: "1"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Series Book (1)/book.epub"},
	)
	standalone := book.NewBook(
		book.Bib{Title: "Standalone", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Standalone (2)/book.epub"},
	)

	storeInIndex(t, idx, seriesBook)
	storeInIndex(t, idx, standalone)

	got, err := idx.Search(Query{Series: []string{"My Series"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Series Book" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Series Book")
	}
}

func TestQueryMultipleFilters(t *testing.T) {
	idx := openTestIndex(t)

	bobRead := book.NewBook(
		book.Bib{Title: "Bob Read", Authors: []book.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 1, Status: "read"},
		book.Location{EpubPath: "B/Bob Read (1)/book.epub"},
	)
	bobUnread := book.NewBook(
		book.Bib{Title: "Bob Unread", Authors: []book.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 2, Status: "unread"},
		book.Location{EpubPath: "B/Bob Unread (2)/book.epub"},
	)
	aliceRead := book.NewBook(
		book.Bib{Title: "Alice Read", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 3, Status: "read"},
		book.Location{EpubPath: "A/Alice Read (3)/book.epub"},
	)

	storeInIndex(t, idx, bobRead)
	storeInIndex(t, idx, bobUnread)
	storeInIndex(t, idx, aliceRead)

	// Filter by Bob AND read.
	got, err := idx.Search(Query{Authors: []string{"Bob"}, Status: []string{"read"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Bob Read" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Bob Read")
	}
}

func TestQueryLimit(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Aardvark"))
	storeInIndex(t, idx, newBook(2, "Beetle"))
	storeInIndex(t, idx, newBook(3, "Cougar"))

	got, err := idx.Search(Query{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestQueryRecentOrder(t *testing.T) {
	idx := openTestIndex(t)

	old := book.NewBook(
		book.Bib{Title: "Old", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Old (1)/book.epub"},
	)
	new := book.NewBook(
		book.Bib{Title: "New", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/New (2)/book.epub"},
	)

	storeInIndex(t, idx, old)
	storeInIndex(t, idx, new)

	got, err := idx.Search(Query{Order: OrderDateAdded})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// OrderDateAdded means date_added DESC. Both books land in the same second
	// here (date_added is RFC3339), so this also pins the id DESC tiebreak.
	if got[0].Meta.ID != 2 {
		t.Errorf("first book id = %d, want 2 (most recently added)", got[0].Meta.ID)
	}
}

func TestQueryEmptyResult(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Only Book"))

	got, err := idx.Search(Query{Status: []string{"abandoned"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestQueryIDNotFound(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Only"))

	got, err := idx.Search(Query{IDs: []int64{999}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}
