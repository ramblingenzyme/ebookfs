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

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
)

// groupingView is one by-x view, described well enough to drive the table
// above against it.
type groupingView struct {
	name   string
	newDir func(*registry.BookRegistry) fs.Dir
	// withKeys is never handed more than one key for a single-valued property.
	withKeys func(id int64, title string, keys ...string) *library.Book
	// keyless is nil for a property every book carries.
	keyless func(id int64, title string) *library.Book
	// entryName is nil when a book appears under its title unchanged.
	entryName func(id int64, title string) string
	// multiKey reports whether the property can hold more than one value.
	multiKey bool
}

var groupingViews = []groupingView{
	{
		name:   "by-author",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewByAuthorDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			return util.MakeBook(id, title, keys...)
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
			return makeBookWithSeries(id, title, "Author", keys[0], strconv.FormatInt(id, 10))
		},
		keyless: func(id int64, title string) *library.Book { return util.MakeBook(id, title, "Author") },
		// Series entries lead with the index so a plain readdir reads in order.
		entryName: func(id int64, title string) string { return fmt.Sprintf("%d - %s", id, title) },
	},
	{
		name:   "by-tag",
		newDir: func(reg *registry.BookRegistry) fs.Dir { return NewByTagDir(reg) },
		withKeys: func(id int64, title string, keys ...string) *library.Book {
			return makeBookWithTags(id, title, "Author", keys...)
		},
		keyless:  func(id int64, title string) *library.Book { return makeBookWithTags(id, title, "Author") },
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

// A group directory name cannot contain a path separator. Author, series and
// tag values reach these views verbatim, so a '/' in one arrives intact, and an
// entry carrying it is one a 9P client can never walk to, which hides every
// book filed under it. keyedDir.entryNames is the single place that guards it.
func TestGroupNamesAreOneComponent(t *testing.T) {
	for _, v := range groupingViews {
		// Statuses are a fixed set, so none carries a '/'.
		if v.name == "by-status" {
			continue
		}
		t.Run(v.name, func(t *testing.T) {
			reg := newTestRegistry(t)
			d := v.newDir(reg)
			reg.Load(v.withKeys(1, "Title", "either/or"))

			names := fstest.ChildNames(d)
			if len(names) != 1 {
				t.Fatalf("groups = %v, want one", names)
			}
			if strings.Contains(names[0], "/") {
				t.Errorf("group name = %q, want no path separator", names[0])
			}

			// Remove has to mint the same name or the group is orphaned.
			removeBookFromView(t, reg, d, 1)
			fstest.ChildCount(t, d, 0)
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
				reg.Load(v.withKeys(1, "My Book", "alpha"))

				wantGroup(t, d, "alpha", v.entry(1, "My Book"))
			})

			t.Run("distinct keys make distinct groups", func(t *testing.T) {
				reg, d := setup(t)
				reg.Load(v.withKeys(1, "First", "alpha"))
				reg.Load(v.withKeys(2, "Second", "beta"))

				wantGroup(t, d, "alpha", v.entry(1, "First"))
				wantGroup(t, d, "beta", v.entry(2, "Second"))
			})

			t.Run("one key gathers several books", func(t *testing.T) {
				reg, d := setup(t)
				reg.Load(v.withKeys(1, "Book A", "alpha"))
				reg.Load(v.withKeys(2, "Book B", "alpha"))

				wantGroup(t, d, "alpha", v.entry(1, "Book A"), v.entry(2, "Book B"))
			})

			t.Run("the last book out prunes the group", func(t *testing.T) {
				reg, d := setup(t)
				reg.Load(v.withKeys(1, "Only Book", "alpha"))
				removeBookFromView(t, reg, d, 1)

				if _, ok := groupEntries(t, d, "alpha"); ok {
					t.Errorf("alpha group outlived its last book; view holds %v", fstest.ChildNames(d))
				}
			})

			t.Run("removing one book leaves the rest", func(t *testing.T) {
				reg, d := setup(t)
				reg.Load(v.withKeys(1, "Keep", "alpha"))
				reg.Load(v.withKeys(2, "Remove", "alpha"))
				removeBookFromView(t, reg, d, 2)

				wantGroup(t, d, "alpha", v.entry(1, "Keep"))
			})

			t.Run("re-keying moves the book", func(t *testing.T) {
				reg, d := setup(t)
				reg.Load(v.withKeys(1, "Moved", "alpha"))
				// The view sees the old and new snapshot around Remove/Add.
				dir := removeBookFromView(t, reg, d, 1)
				dir.SetSnapshot(v.withKeys(1, "Moved", "beta"))
				d.(registry.BookView).Add(dir)

				if _, ok := groupEntries(t, d, "alpha"); ok {
					t.Errorf("alpha group outlived the re-key; view holds %v", fstest.ChildNames(d))
				}
				wantGroup(t, d, "beta", v.entry(1, "Moved"))
			})

			if v.multiKey {
				t.Run("a book joins every group it keys into", func(t *testing.T) {
					reg, d := setup(t)
					reg.Load(v.withKeys(1, "Joint Work", "alpha", "beta"))

					wantGroup(t, d, "alpha", v.entry(1, "Joint Work"))
					wantGroup(t, d, "beta", v.entry(1, "Joint Work"))
				})
			}

			if v.keyless != nil {
				t.Run("a book with no key joins nothing", func(t *testing.T) {
					reg, d := setup(t)
					reg.Load(v.keyless(1, "Unfiled"))

					fstest.ChildCount(t, d, 0)
				})

				t.Run("removing a book with no key is a no-op", func(t *testing.T) {
					reg, d := setup(t)
					reg.Load(v.keyless(1, "Unfiled"))
					removeBookFromView(t, reg, d, 1)
				})
			}
		})
	}
}

func (v groupingView) entry(id int64, title string) string {
	if v.entryName == nil {
		return title
	}
	return v.entryName(id, title)
}

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
	return fstest.ChildNames(group), true
}

// wantGroup checks the key's group lists exactly want, in any order.
func wantGroup(t *testing.T, d fs.Dir, key string, want ...string) {
	t.Helper()
	got, ok := groupEntries(t, d, key)
	if !ok {
		t.Fatalf("no %q group; view holds %v", key, fstest.ChildNames(d))
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("%s group = %v, want %v", key, got, want)
	}
}
