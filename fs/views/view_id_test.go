package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestIDEntryName(t *testing.T) {
	tests := []struct {
		name string
		book *model.Book
		pad  int
		want string
	}{
		{
			name: "no padding for small id",
			book: &model.Book{Bib: model.Bib{Title: "Test"}, Meta: model.Meta{ID: 1}},
			want: "1. Test",
		},
		{
			name: "no padding when pad is 0",
			book: &model.Book{Bib: model.Bib{Title: "Test"}, Meta: model.Meta{ID: 42}},
			pad:  0,
			want: "42. Test",
		},
		{
			name: "two-digit padding",
			book: &model.Book{Bib: model.Bib{Title: "Padded"}, Meta: model.Meta{ID: 5}},
			pad:  2,
			want: "05. Padded",
		},
		{
			name: "two-digit padding at boundary",
			book: &model.Book{Bib: model.Bib{Title: "Boundary"}, Meta: model.Meta{ID: 10}},
			pad:  2,
			want: "10. Boundary",
		},
		{
			name: "three-digit padding",
			book: &model.Book{Bib: model.Bib{Title: "Large"}, Meta: model.Meta{ID: 42}},
			pad:  3,
			want: "042. Large",
		},
		{
			name: "four-digit padding",
			book: &model.Book{Bib: model.Bib{Title: "Huge"}, Meta: model.Meta{ID: 1}},
			pad:  4,
			want: "0001. Huge",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := idEntryName(tc.book, tc.pad)
			if got != tc.want {
				t.Errorf("idEntryName = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIDEntryName_PadTriggeredByMaxID(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b1 := makeBook(1, "First", "Author")
	b2 := makeBook(10, "Tenth", "Author")

	reg.Add(b1)
	reg.Add(b2)

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 books, got %d", len(children))
	}
	want := map[string]bool{"01. First": true, "10. Tenth": true}
	for _, name := range children {
		if !want[name] {
			t.Errorf("unexpected entry %q, want one of %v", name, want)
		}
	}
}

func TestIDEntryName_PadThreeDigits(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b1 := makeBook(1, "First", "Author")
	b2 := makeBook(100, "Hundredth", "Author")

	reg.Add(b1)
	reg.Add(b2)

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 books, got %d", len(children))
	}
	want := map[string]bool{"001. First": true, "100. Hundredth": true}
	for _, name := range children {
		if !want[name] {
			t.Errorf("unexpected entry %q, want one of %v", name, want)
		}
	}
}
