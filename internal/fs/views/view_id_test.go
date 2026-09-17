package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
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
			book: testutil.MakeBook(1, "Test", "Author"),
			want: "1. Test",
		},
		{
			name: "no padding when pad is 0",
			book: testutil.MakeBook(42, "Test", "Author"),
			pad:  0,
			want: "42. Test",
		},
		{
			name: "two-digit padding",
			book: testutil.MakeBook(5, "Padded", "Author"),
			pad:  2,
			want: "05. Padded",
		},
		{
			name: "two-digit padding at boundary",
			book: testutil.MakeBook(10, "Boundary", "Author"),
			pad:  2,
			want: "10. Boundary",
		},
		{
			name: "three-digit padding",
			book: testutil.MakeBook(42, "Large", "Author"),
			pad:  3,
			want: "042. Large",
		},
		{
			name: "four-digit padding",
			book: testutil.MakeBook(1, "Huge", "Author"),
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

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

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

func TestByIDDirAdd(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(wrapBook(b))

	if _, ok := d.Children()["1. Test"]; !ok {
		t.Errorf("by-id should contain '1. Test', got: %v", dirChildNames(d))
	}
}

func TestByIDDirRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if _, ok := d.Children()["1. Test"]; ok {
		t.Error("by-id should not contain entry after remove")
	}
}

func TestByIDDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	reg.Add(testutil.MakeBook(1, "Alpha", "Author"))
	reg.Add(testutil.MakeBook(2, "Beta", "Author"))

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(children), children)
	}
}

// TestByIDDirRemoveUnknown: as above, with a book present so the no-op is
// observable rather than inferred from the absence of a panic.
func TestByIDDirRemoveUnknown(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)
	reg.Add(testutil.MakeBook(1, "Kept", "Author"))

	reg.Remove(999)

	if _, ok := d.Children()["1. Kept"]; !ok {
		t.Errorf("removing an unknown id disturbed the registered books: %v", dirChildNames(d))
	}
}

func TestByIDDirTitleChangeReflected(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByIDDir(reg)

	b := makeBook(1, "Original", "Author")
	reg.Add(wrapBook(b))

	if _, ok := d.Children()["1. Original"]; !ok {
		t.Fatal("by-id should contain '1. Original'")
	}

	// Remove and re-add with different title (simulating an edit)
	reg.Remove(1)
	b2 := makeBook(1, "Updated", "Author")
	reg.Add(wrapBook(b2))

	if _, ok := d.Children()["1. Updated"]; !ok {
		t.Error("by-id should contain '1. Updated' after re-add")
	}
	if _, ok := d.Children()["1. Original"]; ok {
		t.Error("by-id should not contain '1. Original' after update")
	}
}
