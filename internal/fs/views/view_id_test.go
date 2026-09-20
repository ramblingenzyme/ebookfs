package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestIDEntryName(t *testing.T) {
	tests := []struct {
		name string
		book *library.Book
		pad  int
		want string
	}{
		{
			name: "no padding for small id",
			book: util.MakeBook(1, "Test", "Author"),
			want: "1. Test",
		},
		{
			name: "no padding when pad is 0",
			book: util.MakeBook(42, "Test", "Author"),
			pad:  0,
			want: "42. Test",
		},
		{
			name: "two-digit padding",
			book: util.MakeBook(5, "Padded", "Author"),
			pad:  2,
			want: "05. Padded",
		},
		{
			name: "two-digit padding at boundary",
			book: util.MakeBook(10, "Boundary", "Author"),
			pad:  2,
			want: "10. Boundary",
		},
		{
			name: "three-digit padding",
			book: util.MakeBook(42, "Large", "Author"),
			pad:  3,
			want: "042. Large",
		},
		{
			name: "four-digit padding",
			book: util.MakeBook(1, "Huge", "Author"),
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

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	fstest.ChildCount(t, d, 2)
	fstest.HasChild(t, d, "01. First")
	fstest.HasChild(t, d, "10. Tenth")
}

func TestIDEntryName_PadThreeDigits(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b1 := makeBook(1, "First", "Author")
	b2 := makeBook(100, "Hundredth", "Author")

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	fstest.ChildCount(t, d, 2)
	fstest.HasChild(t, d, "001. First")
	fstest.HasChild(t, d, "100. Hundredth")
}

func TestByIDDirAdd(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(wrapBook(b))

	fstest.HasChild(t, d, "1. Test")
}

func TestByIDDirRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(wrapBook(b))
	reg.Remove(1)

	fstest.NoChild(t, d, "1. Test")
}

func TestByIDDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	reg.Add(util.MakeBook(1, "Alpha", "Author"))
	reg.Add(util.MakeBook(2, "Beta", "Author"))

	fstest.ChildCount(t, d, 2)
}

// Removing an unknown id is a no-op. A book is registered so the no-op is
// observable rather than inferred from the absence of a panic.
func TestByIDDirRemoveUnknown(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)
	reg.Add(util.MakeBook(1, "Kept", "Author"))

	reg.Remove(999)

	fstest.HasChild(t, d, "1. Kept")
}

func TestByIDDirTitleChangeReflected(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Original", "Author")
	reg.Add(wrapBook(b))

	fstest.HasChild(t, d, "1. Original")

	// Remove and re-add with different title (simulating an edit)
	reg.Remove(1)
	b2 := makeBook(1, "Updated", "Author")
	reg.Add(wrapBook(b2))

	fstest.HasChild(t, d, "1. Updated")
	fstest.NoChild(t, d, "1. Original")
}
