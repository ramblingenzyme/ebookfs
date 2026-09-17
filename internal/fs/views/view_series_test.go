package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
)

func TestSeriesEntryName(t *testing.T) {
	tests := []struct {
		name string
		book *library.Book
		pad  int32
		want string
	}{
		{
			name: "simple integer index",
			book: makeBookWithSeries(1, "Test", "Author", "", "1"),
			want: "1 - Test",
		},
		{
			name: "decimal index",
			book: makeBookWithSeries(1, "Longer", "Author", "", "2.5"),
			want: "2.5 - Longer",
		},
		{
			name: "zero-padded when maxIdx >= 10",
			book: makeBookWithSeries(1, "Padded", "Author", "", "10"),
			pad:  2,
			want: "10 - Padded",
		},
		{
			name: "zero-padded with decimal",
			book: makeBookWithSeries(1, "DPadded", "Author", "", "10.5"),
			pad:  2,
			want: "10.5 - DPadded",
		},
		{
			name: "index 0",
			book: makeBookWithSeries(1, "Zero", "Author", "", "0"),
			want: "0 - Zero",
		},
		{
			name: "single digit is zero-padded",
			book: makeBookWithSeries(1, "Nine", "Author", "", "9"),
			pad:  2,
			want: "09 - Nine",
		},
		{
			// The index is the string the epub carries and is used as written;
			// it used to be rounded to one decimal place by the float
			// formatting this went through.
			name: "long fractional index passes through",
			book: makeBookWithSeries(1, "Frac", "Author", "", "3.14159"),
			want: "3.14159 - Frac",
		},
		{
			// EPUB 3.3 D.3.7 multi-level position: only the first level is
			// padded, the rest is carried through untouched.
			name: "multi-level position",
			book: makeBookWithSeries(1, "Multi", "Author", "", "2.2.1"),
			pad:  2,
			want: "02.2.1 - Multi",
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

func TestSeriesEntryName_PadTriggeredByMaxIndex(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewBySeriesDir(reg)

	b1 := makeBook(1, "First", "Author")
	b1.Series = &library.Series{Name: "S", Index: "1"}
	b2 := makeBook(2, "Tenth", "Author")
	b2.Series = &library.Series{Name: "S", Index: "10"}

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	sd := fstest.ChildAs[*seriesBookListDir](t, d, "S")

	// With maxIdx >= 10, lower-indexed books are zero-padded.
	fstest.ChildCount(t, sd, 2)
	fstest.HasChild(t, sd, "01 - First")
	fstest.HasChild(t, sd, "10 - Tenth")
}

func TestBySeriesDirRemoveNilSeriesNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewBySeriesDir(reg)

	b := testutil.MakeBook(1, "No Series", "Author")
	bd := book.NewBookDir(newTestFS(t), libfake.Lib{}, func(int64, library.Edits) error { return nil }, b)

	d.Remove(bd) // Should not panic — early return when Series is nil
}
