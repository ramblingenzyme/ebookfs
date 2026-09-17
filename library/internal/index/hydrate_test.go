package index

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestQueryLoadsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:       "Identified",
			Authors:     []book.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-3-16-148410-0", "uuid": "abc-def"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Identified (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	books, err := idx.Search(Query{IDs: []int64{1}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len = %d, want 1", len(books))
	}
	if books[0].Identifiers["isbn"] != "978-3-16-148410-0" {
		t.Errorf("Identifier isbn = %q", books[0].Identifiers["isbn"])
	}
	if books[0].Identifiers["uuid"] != "abc-def" {
		t.Errorf("Identifier uuid = %q", books[0].Identifiers["uuid"])
	}
}

func TestQueryBatchLoadsMultipleBooks(t *testing.T) {
	idx := openTestIndex(t)

	// Three books with distinct authors, tags, and identifiers — the batch
	// loading path must hydrate each book correctly from the same three
	// batch queries, not misattribute rows between books.
	books := []*book.Book{
		book.NewBook(
			book.Bib{
				Title:       "Alpha",
				Authors:     []book.Author{{Name: "Alice", SortName: "Alice"}, {Name: "Ariel", SortName: "Ariel"}},
				Identifiers: map[string]string{"isbn": "111"},
			},
			book.Meta{ID: 1, Tags: []string{"fiction", "sci-fi"}},
			book.Location{EpubPath: "A/Alpha (1)/alpha.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:       "Beta",
				Authors:     []book.Author{{Name: "Bob", SortName: "Bob"}},
				Identifiers: map[string]string{"isbn": "222", "doi": "10.1234/beta"},
			},
			book.Meta{ID: 2, Tags: []string{"non-fiction"}},
			book.Location{EpubPath: "B/Beta (2)/beta.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:       "Gamma",
				Authors:     []book.Author{{Name: "Carol", SortName: "Carol"}, {Name: "Charlie", SortName: "Charlie"}, {Name: "Cecil", SortName: "Cecil"}},
				Identifiers: map[string]string{},
			},
			book.Meta{ID: 3},
			book.Location{EpubPath: "C/Gamma (3)/gamma.epub"},
		),
	}
	for _, b := range books {
		storeInIndex(t, idx, b)
	}

	got, err := idx.Search(Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	for _, b := range got {
		switch b.Meta.ID {
		case int64(1):
			if len(b.Authors) != 2 || b.Authors[0].Name != "Alice" || b.Authors[1].Name != "Ariel" {
				t.Errorf("book 1 authors = %v, want [Alice, Ariel]", b.Authors)
			}
			if !slices.Equal(b.Meta.Tags, []string{"fiction", "sci-fi"}) {
				t.Errorf("book 1 tags = %v, want [fiction, sci-fi]", b.Meta.Tags)
			}
			if b.Identifiers["isbn"] != "111" {
				t.Errorf("book 1 isbn = %q, want 111", b.Identifiers["isbn"])
			}
			if len(b.Identifiers) != 1 {
				t.Errorf("book 1 identifiers = %v, want 1 entry", b.Identifiers)
			}
		case int64(2):
			if len(b.Authors) != 1 || b.Authors[0].Name != "Bob" {
				t.Errorf("book 2 authors = %v, want [Bob]", b.Authors)
			}
			if !slices.Equal(b.Meta.Tags, []string{"non-fiction"}) {
				t.Errorf("book 2 tags = %v, want [non-fiction]", b.Meta.Tags)
			}
			if b.Identifiers["isbn"] != "222" || b.Identifiers["doi"] != "10.1234/beta" {
				t.Errorf("book 2 identifiers = %v", b.Identifiers)
			}
		case int64(3):
			if len(b.Authors) != 3 || b.Authors[0].Name != "Carol" || b.Authors[2].Name != "Cecil" {
				t.Errorf("book 3 authors = %v, want [Carol, Charlie, Cecil]", b.Authors)
			}
			if len(b.Meta.Tags) != 0 {
				t.Errorf("book 3 tags = %v, want empty", b.Meta.Tags)
			}
			if len(b.Identifiers) != 0 {
				t.Errorf("book 3 identifiers = %v, want empty", b.Identifiers)
			}
		default:
			t.Errorf("unexpected book id %d", b.Meta.ID)
		}
	}
}

func TestGetReturnsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:       "Getter",
			Authors:     []book.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-1-234-56789-0"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Getter (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Identifiers["isbn"] != "978-1-234-56789-0" {
		t.Errorf("Identifier isbn = %q", got.Identifiers["isbn"])
	}
}
