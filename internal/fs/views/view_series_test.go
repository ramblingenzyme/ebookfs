package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestSeriesEntryName(t *testing.T) {
	tests := []struct {
		name string
		book *library.Book
		pad  int
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
			name: "two-digit index already fills the pad",
			book: makeBookWithSeries(1, "Padded", "Author", "", "10"),
			pad:  2,
			want: "10 - Padded",
		},
		{
			name: "two-digit index with a decimal fills the pad",
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
		{
			// A pad fixed at two digits left "100" sorting ahead of "99".
			name: "three-digit index",
			book: makeBookWithSeries(1, "Hundred", "Author", "", "100"),
			pad:  3,
			want: "100 - Hundred",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := seriesEntryName(tc.book, padAt(tc.pad))
			if got != tc.want {
				t.Errorf("seriesEntryName = %q, want %q", got, tc.want)
			}
		})
	}
}

// A series past 99 volumes widens to three digits. Two was a fixed ceiling, so
// "100" listed ahead of "99".
func TestSeriesEntryName_PadWidensPast99(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewBySeriesDir(reg)

	for i, index := range []string{"9", "99", "100"} {
		b := makeBook(int64(i+1), "Vol"+index, "Author")
		b.Series = &library.Series{Name: "S", Index: index}
		reg.Load(wrapBook(b))
	}

	sd := fstest.ChildAs[*seriesBookListDir](t, d, "S")
	fstest.ChildCount(t, sd, 3)
	fstest.HasChild(t, sd, "009 - Vol9")
	fstest.HasChild(t, sd, "099 - Vol99")
	fstest.HasChild(t, sd, "100 - Vol100")
}
