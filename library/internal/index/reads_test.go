package index

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func TestStatsEmptyIndex(t *testing.T) {
	idx := openTestIndex(t)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Books != 0 || s.Authors != 0 || s.Series != 0 || s.Tags != 0 || s.TotalSize != 0 {
		t.Errorf("Stats on empty index = %+v, want all zero", s)
	}
	if !s.LastAdded.IsZero() || !s.LastModified.IsZero() {
		t.Errorf("Stats on empty index = %+v, want zero timestamps", s)
	}
}

func TestStatsAggregates(t *testing.T) {
	idx := openTestIndex(t)

	b1 := book.NewBook(
		book.Bib{
			Title:   "First",
			Authors: []book.Author{{Name: "Alice", SortName: "Alice"}},
			Series:  &book.SeriesRef{Name: "EPIC", Index: "1"},
		},
		book.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		book.Location{EpubPath: "A/First (1)/book.epub"},
	)
	b2 := book.NewBook(
		book.Bib{
			Title:   "Second",
			Authors: []book.Author{{Name: "Alice", SortName: "Alice"}, {Name: "Bob", SortName: "Bob"}},
			Series:  &book.SeriesRef{Name: "EPIC", Index: "2"},
		},
		book.Meta{ID: 2, Tags: []string{"sci-fi"}},
		book.Location{EpubPath: "A/Second (2)/book.epub"},
	)
	storeInIndexSized(t, idx, b1, 100)
	storeInIndexSized(t, idx, b2, 250)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Books != 2 {
		t.Errorf("Books = %d, want 2", s.Books)
	}
	if s.Authors != 2 {
		t.Errorf("Authors = %d, want 2 (Alice, Bob)", s.Authors)
	}
	if s.Series != 1 {
		t.Errorf("Series = %d, want 1 (EPIC)", s.Series)
	}
	if s.Tags != 2 {
		t.Errorf("Tags = %d, want 2 (sci-fi, space)", s.Tags)
	}
	if s.TotalSize != 350 {
		t.Errorf("TotalSize = %d, want 350", s.TotalSize)
	}
	if s.LastAdded.IsZero() || s.LastModified.IsZero() {
		t.Errorf("Stats = %+v, want non-zero timestamps", s)
	}
}

// TestStatsExcludesOrphans exercises the same orphan cleanup TestPutAuthorsWithExistingName
// relies on: replacing a book's tags must not leave the old tag counted in Stats.
func TestStatsExcludesOrphans(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{Title: "Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"stale"}},
		book.Location{EpubPath: "A/Book (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	updated := book.NewBook(
		book.Bib{Title: "Book", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"fresh"}},
		book.Location{EpubPath: "A/Book (1)/book.epub"},
	)
	storeInIndex(t, idx, updated)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Tags != 1 {
		t.Errorf("Tags = %d, want 1 (stale tag should be swept)", s.Tags)
	}
}
