// One table over every by-x view: by-author, by-series, by-tag, by-status. They
// differ only in which book property keys a group and whether a book can key
// into more than one, so the nine rules they share are asserted once here rather
// than copied into four files.
//
// The rules: a book creates its group, distinct keys make distinct groups, one
// key gathers several books, the last book out prunes the group, removing one
// book leaves the rest, re-keying moves the book, a multi-key book joins every
// group it keys into, and a keyless book joins and leaves nothing.
//
// Behaviour particular to one view stays in that view's paired test file.

package views

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// groupingView describes a directory that files books into subdirectories keyed
// by some property of the book (author, series, tag, status). The four differ
// only in which property they read and how many values it can hold, so the
// behaviour they share (a group appears with its first book, is pruned with its
// last, and follows the book when the property changes) is asserted once here.
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
			b.Series = &library.Series{Name: keys[0], Index: strconv.FormatInt(id, 10)}
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

// A group directory name cannot contain a path separator. Author and series
// names are metadata read verbatim from the epub, so a '/' in one reaches these
// views intact; an entry carrying it is one a 9P client can never walk to, which
// hides every book filed under it. by-tag already guarded this with
// tagEntryName; by-author and by-series did not.
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
				b.Series = &library.Series{Name: "Either/Or", Index: "1"}
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
