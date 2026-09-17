// Registry, views and the per-book directory composed together: an edit written
// to a 9P field file must rehome the book across every view that groups on the
// field it changed. Spans three packages, so it pairs with no single source
// file, and drives only the public 9P path.
//
// Registry-internal behavior (edit on an unknown id, and the concurrent
// snapshot swap) is tested white-box in fs/registry instead, since those tests
// call the unexported edit method.

package fs

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

// writeField drives a field edit the way a 9P client would: open the named
// fieldFile with Otrunc, write the new value, and close to commit.
func writeField(t *testing.T, bd fs.Dir, name, value string) {
	t.Helper()
	fstest.Fid(t, fstest.ChildAs[fs.File](t, bd, name), 1).Set(proto.Otrunc, value)
}

func TestRegistryEditTitleRehomesInAllViews(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Old Title", "Alice")
	book.Meta.Status = "unread"
	// The real library fetches the edit base by id; the fake closes over the
	// test's book instead.
	lib := libfake.Lib{
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			updated := *book
			if e.Title != nil {
				updated.Title = *e.Title
			}
			if e.SortTitle != nil {
				updated.SortTitle = *e.SortTitle
			}
			updated.Meta.DateModified = book.Meta.DateModified
			return testutil.WrapBook(&updated), nil
		},
	}
	reg := registry.NewBookRegistry(f, lib)

	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	byID := views.NewByIDDir(reg)

	reg.Add(testutil.WrapBook(book))

	writeField(t, fstest.ChildAs[fs.Dir](t, allBooks, "Old Title"), "title", "New Title")

	fstest.HasChild(t, allBooks, "New Title")
	fstest.NoChild(t, allBooks, "Old Title")
	fstest.HasChild(t, byID, "1. New Title")

	// The author did not change, so the book stays under Alice.
	fstest.HasChild(t, fstest.ChildAs[fs.Dir](t, byAuthor, "Alice"), "New Title")
}

func TestRegistryEditAuthorsRehomesInByAuthor(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Alice")
	lib := libfake.Lib{
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			updated := *book
			if e.Authors != nil {
				updated.Authors = *e.Authors
			}
			updated.Meta.DateModified = book.Meta.DateModified
			return testutil.WrapBook(&updated), nil
		},
	}
	reg := registry.NewBookRegistry(f, lib)

	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)

	reg.Add(testutil.WrapBook(book))

	writeField(t, fstest.ChildAs[fs.Dir](t, allBooks, "Test"), "authors", "Bob")

	fstest.HasChild(t, fstest.ChildAs[fs.Dir](t, byAuthor, "Bob"), "Test")
	fstest.NoChild(t, byAuthor, "Alice")
}

func TestRegistryEditStatusChangesReaderView(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author1")
	book.EpubPath = "Test.epub"
	book.Meta.Status = "unread"
	lib := libfake.Lib{
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			updated := *book
			if e.Status != nil {
				updated.Meta.Status = *e.Status
			}
			updated.Meta.DateModified = book.Meta.DateModified
			return testutil.WrapBook(&updated), nil
		},
	}
	reg := registry.NewBookRegistry(f, lib)
	allBooks := views.NewAllBooksDir(reg)
	readerDir := views.NewReaderDir(reg, libfake.Exporter{StatusList: []string{"reading"}})

	reg.Add(testutil.WrapBook(book))

	// Reader view should not show the book when status is "unread".
	fstest.ChildCount(t, readerDir, 0)

	writeField(t, fstest.ChildAs[fs.Dir](t, allBooks, "Test"), "status", "reading")

	fstest.HasChild(t, fstest.ChildAs[fs.ModDir](t, readerDir, "Author1"), "Test.epub")
}
