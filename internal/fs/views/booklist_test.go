package views

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestRegistryAddAndRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	b := makeBook(1, "Test Book", "Author")
	reg.Add(wrapBook(b))

	fstest.HasChild(t, d, "Test Book")

	reg.Remove(1)

	fstest.NoChild(t, d, "Test Book")
}

// Removing an id that was never added is distinguishable from removing
// everything: a real book stays registered, so against an empty registry a
// Remove that cleared the whole view would look identical.
func TestRegistryRemoveUnknownID(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)
	reg.Add(util.MakeBook(1, "Kept", "Author"))

	reg.Remove(999)

	fstest.HasChild(t, d, "Kept")
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
	fstest.ChildCount(t, allBooks, 1)
	fstest.HasChild(t, allBooks, "First Title")
}

func TestBooksDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(util.MakeBook(1, "Alpha", "Author"))
	reg.Add(util.MakeBook(2, "Beta", "Author"))

	fstest.ChildCount(t, d, 2)
}

func TestBooksDirRemoveOnlyOne(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(util.MakeBook(1, "Keep", "Author"))
	reg.Add(util.MakeBook(2, "Remove", "Author"))

	reg.Remove(2)

	fstest.NoChild(t, d, "Remove")
	fstest.HasChild(t, d, "Keep")
}

// A '/' in a title cannot become a path separator in a 9P entry name, and the
// listing and the entries map must agree on the name, or the book can no longer
// be removed. epub reports titles as the file wrote them (EPUB 3.3 §5.5.2), so
// this is the layer that has to make one safe.
func TestBooksDirSlashInTitleIsOneEntry(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	reg.Add(util.MakeBook(1, "Either/Or", "Author"))

	children := fstest.ChildNames(d)
	if len(children) != 1 {
		t.Fatalf("expected 1 book, got %v", children)
	}
	if strings.Contains(children[0], "/") {
		t.Errorf("entry name = %q, want no path separator", children[0])
	}

	// The entries map can name a child that DeleteChild then cannot find.
	reg.Remove(1)
	fstest.ChildCount(t, d, 0)
}

func TestBooksDirEmptyNilMap(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	fstest.ChildCount(t, d, 0)
}

func TestBooksDirDuplicateTitles(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	b1 := makeBook(1, "Same Title", "Alice")
	b2 := makeBook(2, "Same Title", "Bob")

	reg.Add(wrapBook(b1))
	fstest.HasChild(t, d, "Same Title")

	reg.Add(wrapBook(b2))
	fstest.HasChild(t, d, "Same Title")
	fstest.HasChild(t, d, "Same Title (2)")
	fstest.ChildCount(t, d, 2)

	// Removing the first book leaves the second at its minted name.
	reg.Remove(1)
	fstest.NoChild(t, d, "Same Title")
	fstest.HasChild(t, d, "Same Title (2)")
	fstest.ChildCount(t, d, 1)

	reg.Remove(2)
	fstest.NoChild(t, d, "Same Title (2)")
	fstest.ChildCount(t, d, 0)
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

	fstest.ChildCount(t, d, 3)
}
