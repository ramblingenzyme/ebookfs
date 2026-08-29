package views

import (
	"fmt"
	"github.com/ramblingenzyme/ebookfs/library"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ---- Registry ----

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

// TestRegistryRemoveUnknownID keeps a real book registered, so removing an id
// that was never added is distinguishable from removing everything — against an
// empty registry a Remove that cleared the whole view would look identical.
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

	// dirLocked returns the existing dir and doesn't update the book pointer,
	// so the first title persists. The caller is expected to not reuse IDs.
	children := dirChildNames(allBooks)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d: %v", len(children), children)
	}
	if children[0] != "First Title" {
		t.Errorf("expected 'First Title', got %q", children[0])
	}
}

// ---- BooksDir ----

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

// TestBooksDirSlashInTitleIsOneEntry pins that a '/' in a title cannot become a
// path separator in a 9P entry name, and — the part that would fail silently —
// that the listing and the entries map agree on the name, so the book can still
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

// TestGroupNamesAreOneComponent pins that a group directory name cannot contain
// a path separator. Author and series names are metadata read verbatim from the
// epub, so a '/' in one reaches these views intact; an entry carrying it is one
// a 9P client can never walk to, which hides every book filed under it. by-tag
// already guarded this with tagEntryName; by-author and by-series did not.
func TestGroupNamesAreOneComponent(t *testing.T) {
	for _, tc := range []struct {
		name string
		dir  func(*registry.BookRegistry) fs.Dir
		book func() *library.Book
	}{
		{
			name: "by-author",
			dir:  func(reg *registry.BookRegistry) fs.Dir { return NewByAuthorDir(reg) },
			book: func() *library.Book { return testutil.MakeBook(1, "Title", "Doe/Jane") },
		},
		{
			name: "by-series",
			dir:  func(reg *registry.BookRegistry) fs.Dir { return NewBySeriesDir(reg) },
			book: func() *library.Book {
				b := makeBook(1, "Title", "Author")
				b.Series = &model.SeriesRef{Name: "Either/Or", Index: "1"}
				return wrapBook(b)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reg := newTestRegistry(t)
			d := tc.dir(reg)
			reg.Add(tc.book())

			names := dirChildNames(d)
			if len(names) != 1 {
				t.Fatalf("groups = %v, want one", names)
			}
			if strings.Contains(names[0], "/") {
				t.Errorf("group name = %q, want no path separator", names[0])
			}

			// Remove has to mint the same name or the group is orphaned.
			reg.Remove(1)
			if got := dirChildNames(d); len(got) != 0 {
				t.Errorf("after remove: %v, want the group pruned", got)
			}
		})
	}
}

func TestBooksDirEmptyNilMap(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewAllBooksDir(reg)

	if n := len(d.Children()); n != 0 {
		t.Errorf("new books dir should be empty, got %d children", n)
	}
}

// ---- Grouping views ----

// groupingView describes a directory that files books into subdirectories keyed
// by some property of the book — author, series, tag, status. The four differ
// only in which property they read and how many values it can hold, so the
// behaviour they share (a group appears with its first book, is pruned with its
// last, and follows the book when the property changes) is asserted once here
// rather than restated four times over.
type groupingView struct {
	name   string
	newDir func(*registry.BookRegistry) fs.Dir
	// withKeys returns a book this view files under each of keys. A view whose
	// property holds a single value is never handed more than one.
	withKeys func(id int64, title string, keys ...string) *library.Book
	// keyless returns a book the view files nowhere, or nil for a property
	// every book carries.
	keyless func(id int64, title string) *library.Book
	// entryName is the name a book appears under inside its group; nil means
	// the title unchanged.
	entryName func(id int64, title string) string
	// multiKey reports whether the property can hold more than one value.
	multiKey bool
}

// entry is the name title appears under inside one of v's groups.
func (v groupingView) entry(id int64, title string) string {
	if v.entryName == nil {
		return title
	}
	return v.entryName(id, title)
}

var groupingViews = []groupingView{
	{
		name:   "by-author",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewByAuthorDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			return testutil.MakeBook(id, title, keys...)
		},
		keyless: func(id int64, title string) *library.Book {
			b := makeBook(id, title)
			b.Authors = nil
			return wrapBook(b)
		},
		multiKey: true,
	},
	{
		name:   "by-series",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewBySeriesDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			b := makeBook(id, title, "Author")
			b.Series = &model.SeriesRef{Name: keys[0], Index: strconv.FormatInt(id, 10)}
			return wrapBook(b)
		},
		keyless: func(id int64, title string) *library.Book { return testutil.MakeBook(id, title, "Author") },
		// Series entries lead with the index so a plain readdir reads in order.
		entryName: func(id int64, title string) string { return fmt.Sprintf("%d - %s", id, title) },
	},
	{
		name:   "by-tag",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewByTagDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			b := makeBook(id, title, "Author")
			b.Meta.Tags = keys
			return wrapBook(b)
		},
		keyless: func(id int64, title string) *library.Book {
			b := makeBook(id, title, "Author")
			b.Meta.Tags = nil
			return wrapBook(b)
		},
		multiKey: true,
	},
	{
		name:   "by-status",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewByStatusDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			b := makeBook(id, title, "Author")
			b.Meta.Status = keys[0]
			return wrapBook(b)
		},
		// Every book carries a status, so there is no keyless case.
	},
}

// groupEntries returns the entries filed under the group directory named key,
// and whether that group exists at all.
func groupEntries(t *testing.T, d fs.Dir, key string) ([]string, bool) {
	t.Helper()
	child, ok := d.Children()[key]
	if !ok {
		return nil, false
	}
	group, ok := child.(fs.Dir)
	if !ok {
		t.Fatalf("group %q is a %T, want a directory", key, child)
	}
	names := dirChildNames(group)
	slices.Sort(names)
	return names, true
}

// mustGroupEntries is groupEntries where a missing group fails the test.
func mustGroupEntries(t *testing.T, d fs.Dir, key string) []string {
	t.Helper()
	names, ok := groupEntries(t, d, key)
	if !ok {
		t.Fatalf("no %q group; view holds %v", key, dirChildNames(d))
	}
	return names
}

func TestGroupingViews(t *testing.T) {
	for _, v := range groupingViews {
		t.Run(v.name, func(t *testing.T) {
			setup := func(t *testing.T) (*registry.BookRegistry, fs.Dir) {
				t.Helper()
				reg := newTestRegistry(t)
				return reg, v.newDir(reg)
			}

			t.Run("a book creates its group", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "My Book", "alpha"))

				want := []string{v.entry(1, "My Book")}
				if got := mustGroupEntries(t, d, "alpha"); !slices.Equal(got, want) {
					t.Errorf("alpha group = %v, want %v", got, want)
				}
			})

			t.Run("distinct keys make distinct groups", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "First", "alpha"))
				reg.Add(v.withKeys(2, "Second", "beta"))

				want := []string{v.entry(1, "First")}
				if got := mustGroupEntries(t, d, "alpha"); !slices.Equal(got, want) {
					t.Errorf("alpha group = %v, want %v", got, want)
				}
				want = []string{v.entry(2, "Second")}
				if got := mustGroupEntries(t, d, "beta"); !slices.Equal(got, want) {
					t.Errorf("beta group = %v, want %v", got, want)
				}
			})

			t.Run("one key gathers several books", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "Book A", "alpha"))
				reg.Add(v.withKeys(2, "Book B", "alpha"))

				want := []string{v.entry(1, "Book A"), v.entry(2, "Book B")}
				slices.Sort(want)
				if got := mustGroupEntries(t, d, "alpha"); !slices.Equal(got, want) {
					t.Errorf("alpha group = %v, want %v", got, want)
				}
			})

			t.Run("the last book out prunes the group", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "Only Book", "alpha"))
				reg.Remove(1)

				if _, ok := groupEntries(t, d, "alpha"); ok {
					t.Errorf("alpha group outlived its last book; view holds %v", dirChildNames(d))
				}
			})

			t.Run("removing one book leaves the rest", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "Keep", "alpha"))
				reg.Add(v.withKeys(2, "Remove", "alpha"))
				reg.Remove(2)

				want := []string{v.entry(1, "Keep")}
				if got := mustGroupEntries(t, d, "alpha"); !slices.Equal(got, want) {
					t.Errorf("alpha group = %v, want %v", got, want)
				}
			})

			t.Run("re-keying moves the book", func(t *testing.T) {
				reg, d := setup(t)
				reg.Add(v.withKeys(1, "Moved", "alpha"))
				// An edit reaches the views as a remove followed by an add.
				reg.Remove(1)
				reg.Add(v.withKeys(1, "Moved", "beta"))

				if _, ok := groupEntries(t, d, "alpha"); ok {
					t.Errorf("alpha group outlived the re-key; view holds %v", dirChildNames(d))
				}
				want := []string{v.entry(1, "Moved")}
				if got := mustGroupEntries(t, d, "beta"); !slices.Equal(got, want) {
					t.Errorf("beta group = %v, want %v", got, want)
				}
			})

			if v.multiKey {
				t.Run("a book joins every group it keys into", func(t *testing.T) {
					reg, d := setup(t)
					reg.Add(v.withKeys(1, "Joint Work", "alpha", "beta"))

					want := []string{v.entry(1, "Joint Work")}
					for _, key := range []string{"alpha", "beta"} {
						if got := mustGroupEntries(t, d, key); !slices.Equal(got, want) {
							t.Errorf("%s group = %v, want %v", key, got, want)
						}
					}
				})
			}

			if v.keyless != nil {
				t.Run("a book with no key joins nothing", func(t *testing.T) {
					reg, d := setup(t)
					reg.Add(v.keyless(1, "Unfiled"))

					if got := dirChildNames(d); len(got) != 0 {
						t.Errorf("view holds %v, want nothing filed for a book with no key", got)
					}
				})

				t.Run("removing a book with no key is a no-op", func(t *testing.T) {
					reg, _ := setup(t)
					reg.Add(v.keyless(1, "Unfiled"))
					reg.Remove(1) // must not panic
				})
			}
		})
	}
}

// ---- ByIDDir ----

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

// ---- ReaderDir ----

func TestReaderDirAddIncludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "To Read", "Author1")
	b.EpubPath = "To Read.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	ad, ok := d.Children()["Author1"]
	if !ok {
		t.Fatal("reader should have 'Author1' subdir")
	}
	ald := ad.(fs.ModDir)
	if _, ok := ald.Children()["To Read.epub"]; !ok {
		t.Errorf("reader should contain 'To Read.epub' under Author1")
	}
}

func TestReaderDirSkipExcludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Finished", "Author2")
	b.Meta.Status = "read"
	reg.Add(wrapBook(b))

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should have no children for 'read' status book, got %d", n)
	}
}

func TestReaderDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Only", "Author3")
	b.EpubPath = "Only.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should be empty after removing only book, got %d children", n)
	}
}

func TestReaderDirCoAuthorSingleDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	// Co-authored books go under a single "Alice & Bob" folder
	ad, ok := d.Children()["Alice & Bob"]
	if !ok {
		t.Fatalf("reader should have 'Alice & Bob' dir, got: %v", dirChildNames(d))
	}
	ald := ad.(fs.ModDir)
	if _, ok := ald.Children()["Joint.epub"]; !ok {
		t.Errorf("reader should contain 'Joint.epub' under 'Alice & Bob'")
	}
}

func TestReaderDirCoAuthorRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if _, ok := d.Children()["Alice & Bob"]; ok {
		t.Error("'Alice & Bob' dir should be pruned after removal")
	}
}

func TestReaderDirMultipleBooksSameAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b1 := makeBook(1, "Book A", "SameAuthor")
	b1.EpubPath = "A.epub"
	b1.Meta.Status = "unread"

	b2 := makeBook(2, "Book B", "SameAuthor")
	b2.EpubPath = "B.epub"
	b2.Meta.Status = "unread"

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	ad, ok := d.Children()["SameAuthor"]
	if !ok {
		t.Fatal("reader should have 'SameAuthor' subdir with two books")
	}
	ald := ad.(fs.ModDir)
	children := dirChildNames(ald)
	if len(children) != 2 {
		t.Errorf("expected 2 books under SameAuthor, got %d: %v", len(children), children)
	}
}

func TestReaderDirWithConvertEnabled(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Convert Me", "AuthorX")
	b.EpubPath = "Convert.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	ad, ok := d.Children()["AuthorX"]
	if !ok {
		t.Fatal("reader should have 'AuthorX' subdir")
	}
	if _, ok := ad.(fs.ModDir).Children()["Convert.epub"]; !ok {
		t.Error("reader should contain 'Convert.epub'")
	}
}

// ---- PruneEmpty ----

func TestPruneEmptyNoOpForMissingChild(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	g.pruneEmpty("nonexistent")
	// Should not panic.
}

func TestPruneEmptyNoOpForNonEmptyDir(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	child := newBookListDir(newStat(g.f, "child", 0555|proto.DMDIR))
	g.StaticDir.AddChild(child)

	// Add a grandchild so the dir is not empty.
	grandchild := newBookListDir(newStat(g.f, "grandchild", 0555|proto.DMDIR))
	child.AddChild(grandchild)

	g.pruneEmpty("child")
	if _, ok := g.Children()["child"]; !ok {
		t.Error("non-empty child should not be pruned")
	}
}

func TestBySeriesDirRemoveNilSeriesNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewBySeriesDir(reg)

	b := testutil.MakeBook(1, "No Series", "Author")
	bd := book.NewBookDir(newTestFS(t), libfake.Lib{}, func(int64, model.Edits) error { return nil }, b)

	d.Remove(bd) // Should not panic — early return when Series is nil
}

// ---- ByTagDir ----

func TestByTagDirTagWithSlash(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"a/b"}
	reg.Add(wrapBook(b))

	if _, ok := d.Children()["a_b"]; !ok {
		t.Fatalf("by-tag should have 'a_b' subdir for tag 'a/b', got: %v", dirChildNames(d))
	}
	if _, ok := d.Children()["a/b"]; ok {
		t.Error("by-tag should NOT have 'a/b' subdir (slash not valid in 9P names)")
	}
}

func TestByTagDirRemoveWithSlashTag(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"x/y"}
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if _, ok := d.Children()["x_y"]; ok {
		t.Error("tag subdir should be pruned after remove")
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
