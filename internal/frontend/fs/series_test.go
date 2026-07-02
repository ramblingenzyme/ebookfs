package fs

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func TestSeriesEntryName(t *testing.T) {
	tests := []struct {
		name  string
		book  *model.Book
		pad   int
		want  string
	}{
		{
			name: "simple integer index",
			book: &model.Book{Bib: model.Bib{Title: "Test", Series: &model.SeriesRef{Index: 1}}},
			want: "1 - Test",
		},
		{
			name: "decimal index",
			book: &model.Book{Bib: model.Bib{Title: "Longer", Series: &model.SeriesRef{Index: 2.5}}},
			want: "2.5 - Longer",
		},
		{
			name: "zero-padded when maxIdx >= 10",
			book: &model.Book{Bib: model.Bib{Title: "Padded", Series: &model.SeriesRef{Index: 10}}},
			pad:  2,
			want: "10 - Padded",
		},
		{
			name: "zero-padded with decimal",
			book: &model.Book{Bib: model.Bib{Title: "DPadded", Series: &model.SeriesRef{Index: 10.5}}},
			pad:  2,
			want: "10.5 - DPadded",
		},
		{
			name: "index 0",
			book: &model.Book{Bib: model.Bib{Title: "Zero", Series: &model.SeriesRef{Index: 0}}},
			want: "0 - Zero",
		},
		{
			name: "large fractional index",
			book: &model.Book{Bib: model.Bib{Title: "Frac", Series: &model.SeriesRef{Index: 3.14159}}},
			want: "3.1 - Frac",
		},
		{
			name: ".0 trimmed to integer",
			book: &model.Book{Bib: model.Bib{Title: "Trim", Series: &model.SeriesRef{Index: 42.0}}},
			want: "42 - Trim",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := seriesEntryName(tc.book, tc.pad)
			if got != tc.want {
				t.Errorf("seriesEntryName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSeriesEntryNameFunc(t *testing.T) {
	fn := seriesEntryNameFunc(0)
	book := &model.Book{Bib: model.Bib{Title: "Test", Series: &model.SeriesRef{Index: 1}}}
	got := fn(book)
	want := "1 - Test"
	if got != want {
		t.Errorf("seriesEntryNameFunc = %q, want %q", got, want)
	}
}

func TestSeriesEntryName_PadTriggeredByMaxIndex(t *testing.T) {
	f := newTestFS(t)
	reg := newTestRegistry(t, f)
	d := newBySeriesDir(reg)

	b1 := makeBook(1, "First", "Author")
	b1.Series = &model.SeriesRef{Name: "S", Index: 1.0}
	b2 := makeBook(2, "Tenth", "Author")
	b2.Series = &model.SeriesRef{Name: "S", Index: 10.0}

	reg.Add(b1)
	reg.Add(b2)

	sd := d.Children()["S"].(*seriesBookListDir)
	children := dirChildNames(sd)
	if len(children) != 2 {
		t.Fatalf("expected 2 books, got %d", len(children))
	}
	// With maxIdx >= 10, lower-indexed books should be zero-padded.
	want := map[string]bool{"01 - First": true, "10 - Tenth": true}
	for _, name := range children {
		if !want[name] {
			t.Errorf("unexpected entry %q, want one of %v", name, want)
		}
	}
}
