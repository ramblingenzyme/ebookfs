package index

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestPutAuthorsWithExistingName(t *testing.T) {
	idx := openTestIndex(t)

	authors := []book.Author{{Name: "Alice", SortName: "Smith, Alice"}}
	b1 := book.NewBook(
		book.Bib{Title: "First", Authors: authors},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/First (1)/book.epub"},
	)
	storeInIndex(t, idx, b1)

	// Second book with the same author name, triggering the ON CONFLICT upsert.
	b2 := book.NewBook(
		book.Bib{Title: "Second", Authors: authors},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Second (2)/book.epub"},
	)
	storeInIndex(t, idx, b2)

	got, err := idx.Search(Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestPutBookWithSeriesAndTags(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:   "Series Book",
			Authors: []book.Author{{Name: "Alice", SortName: "Alice"}},
			Series:  &book.SeriesRef{Name: "EPIC", Index: "1"},
		},
		book.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		book.Location{EpubPath: "A/Series Book (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Series == nil || got.Series.Name != "EPIC" {
		t.Errorf("series = %+v", got.Series)
	}
	if len(got.Meta.Tags) != 2 {
		t.Errorf("tags = %v", got.Meta.Tags)
	}
}
