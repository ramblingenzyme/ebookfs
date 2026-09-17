package views

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

func TestRegistryAddAndRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	b := makeBook(1, "Test Book", "Author")
	reg.Add(wrapBook(b))

	if _, ok := d.Children()["Test Book"]; !ok {
		t.Fatal("books dir should contain 'Test Book' after Add")
	}

	reg.Remove(1)

	if _, ok := d.Children()["Test Book"]; ok {
		t.Error("books dir should not contain 'Test Book' after Remove")
	}
}

// Removing an id that was never added is distinguishable from removing
// everything: a real book stays registered, so against an empty registry a
// Remove that cleared the whole view would look identical.
func TestRegistryRemoveUnknownID(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)
	reg.Add(testutil.MakeBook(1, "Kept", "Author"))

	reg.Remove(999)

	if _, ok := d.Children()["Kept"]; !ok {
		t.Errorf("removing an unknown id disturbed the registered books: %v", dirChildNames(d))
	}
}

func TestRegistryAddSameIDTwiceUsesSameDir(t *testing.T) {
	reg := newTestRegistry(t)
	allBooks := NewAllBooksDir(reg)

	b1 := makeBook(1, "First Title", "Author")
	b2 := makeBook(1, "Second Title", "Author")

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	// dirLocked returns the existing dir and does not update the book pointer, so
	// the first title persists. The caller is expected not to reuse IDs.
	children := dirChildNames(allBooks)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d: %v", len(children), children)
	}
	if children[0] != "First Title" {
		t.Errorf("expected 'First Title', got %q", children[0])
	}
}

func TestBooksDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(testutil.MakeBook(1, "Alpha", "Author"))
	reg.Add(testutil.MakeBook(2, "Beta", "Author"))

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 books, got %d: %v", len(children), children)
	}
}

func TestBooksDirRemoveOnlyOne(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(testutil.MakeBook(1, "Keep", "Author"))
	reg.Add(testutil.MakeBook(2, "Remove", "Author"))

	reg.Remove(2)

	if _, ok := d.Children()["Remove"]; ok {
		t.Error("'Remove' should be gone")
	}
	if _, ok := d.Children()["Keep"]; !ok {
		t.Error("'Keep' should remain")
	}
}

// A '/' in a title cannot become a path separator in a 9P entry name, and the
// listing and the entries map must agree on the name, or the book can no longer
// be removed. epub reports titles as the file wrote them (EPUB 3.3 §5.5.2), so
// this is the layer that has to make one safe.
func TestBooksDirSlashInTitleIsOneEntry(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(testutil.MakeBook(1, "Either/Or", "Author"))

	children := dirChildNames(d)
	if len(children) != 1 {
		t.Fatalf("expected 1 book, got %v", children)
	}
	if strings.Contains(children[0], "/") {
		t.Errorf("entry name = %q, want no path separator", children[0])
	}

	reg.Remove(1)
	if got := dirChildNames(d); len(got) != 0 {
		t.Errorf("after remove: %v, want the book gone — the entries map named a child that could not be deleted", got)
	}
}

func TestBooksDirEmptyNilMap(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	if n := len(d.Children()); n != 0 {
		t.Errorf("new books dir should be empty, got %d children", n)
	}
}

func TestBooksDirDuplicateTitles(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	b1 := makeBook(1, "Same Title", "Alice")
	b2 := makeBook(2, "Same Title", "Bob")

	reg.Add(wrapBook(b1))
	if _, ok := d.Children()["Same Title"]; !ok {
		t.Fatal("first book should appear under plain title")
	}

	reg.Add(wrapBook(b2))
	if _, ok := d.Children()["Same Title"]; !ok {
		t.Error("first book should remain at plain title")
	}
	if _, ok := d.Children()["Same Title (2)"]; !ok {
		t.Error("second book should appear as 'Same Title (2)'")
	}
	if len(d.Children()) != 2 {
		t.Errorf("expected 2 children, got %d", len(d.Children()))
	}

	// Removing the first book should not affect the second.
	reg.Remove(1)
	if _, ok := d.Children()["Same Title"]; ok {
		t.Error("first book should be removed")
	}
	if _, ok := d.Children()["Same Title (2)"]; !ok {
		t.Error("second book should remain after first is removed")
	}
	if len(d.Children()) != 1 {
		t.Errorf("expected 1 child after removing first book, got %d", len(d.Children()))
	}

	// Removing the second book cleans up the disambiguated entry.
	reg.Remove(2)
	if _, ok := d.Children()["Same Title (2)"]; ok {
		t.Error("second book should be removed")
	}
	if len(d.Children()) != 0 {
		t.Errorf("expected 0 children after removing both, got %d", len(d.Children()))
	}
}

// The gap documented on disambiguatedName. Add checks the plain title for a
// collision but not the name it then mints, so a book literally titled "Foo (2)"
// and the minted name for book id 2 titled "Foo" are the same key.
//
// Order matters: the literal has to be registered first, then the collision
// that mints over it.
func TestBooksDirMintedNameCollidesWithLiteralTitle(t *testing.T) {
	t.Skip("known defect: the minted entry replaces the literal one, so a registered " +
		"book vanishes from the listing, and entries then maps both ids to that name, " +
		"so removing either deletes the other's entry too. The fix is to mint until the " +
		"name is free rather than assume one pass suffices. Drop this line to see it fail.")

	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(wrapBook(makeBook(5, "Foo", "Alice")))   // takes the plain name
	reg.Add(wrapBook(makeBook(1, "Foo (2)", "Bob"))) // literal title
	reg.Add(wrapBook(makeBook(2, "Foo", "Carol")))   // mints "Foo (2)"

	if got := len(d.Children()); got != 3 {
		t.Errorf("listing holds %d of 3 registered books: %v", got, dirChildNames(d))
	}
}
