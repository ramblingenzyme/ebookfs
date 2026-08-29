package fs

import (
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/library"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// writeField drives a field edit the way a 9P client would: open the named
// fieldFile with Otrunc, write the new value, and close to commit.
func writeField(t *testing.T, bd fs.Dir, name, value string) {
	t.Helper()
	ff := bd.Children()[name].(fs.File)
	fid := uint64(1)
	if err := ff.Open(fid, proto.Otrunc); err != nil {
		t.Fatalf("Open %s field: %v", name, err)
	}
	if _, err := ff.Write(fid, 0, []byte(value)); err != nil {
		t.Fatalf("Write %s field: %v", name, err)
	}
	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close %s field: %v", name, err)
	}
}

func TestRegistryEditTitleRehomesInAllViews(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Old Title", "Alice")
	book.Meta.Status = "unread"
	// The real library fetches the edit base by id; the fake closes over the
	// test's book instead.
	lib := libfake.Lib{
		EditFn: func(id int64, e model.Edits) (*library.Book, error) {
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

	// Edit the title via its fieldFile.
	bd := allBooks.Children()["Old Title"].(fs.Dir)
	writeField(t, bd, "title", "New Title")

	// All views should reflect the new title.
	if _, ok := allBooks.Children()["New Title"]; !ok {
		t.Error("allBooks should show 'New Title'")
	}
	if _, ok := allBooks.Children()["Old Title"]; ok {
		t.Error("allBooks should not show 'Old Title'")
	}

	if _, ok := byID.Children()["1. New Title"]; !ok {
		t.Error("by-id should show '1. New Title'")
	}

	// by-author: book should still be under Alice (author unchanged)
	ad, ok := byAuthor.Children()["Alice"]
	if !ok {
		t.Fatal("by-author should still have Alice")
	}
	ald := ad.(fs.Dir)
	if _, ok := ald.Children()["New Title"]; !ok {
		t.Error("Alice's dir should contain 'New Title'")
	}
}

func TestRegistryEditAuthorsRehomesInByAuthor(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Alice")
	lib := libfake.Lib{
		EditFn: func(id int64, e model.Edits) (*library.Book, error) {
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

	// Change authors from Alice to Bob.
	bd := allBooks.Children()["Test"].(fs.Dir)
	writeField(t, bd, "authors", "Bob")

	// Author should now be Bob, Alice pruned.
	if _, ok := byAuthor.Children()["Bob"]; !ok {
		t.Error("by-author should have 'Bob'")
	}
	ad, ok := byAuthor.Children()["Bob"]
	if !ok {
		t.Fatal("by-author should have 'Bob'")
	}
	ald := ad.(fs.Dir)
	if _, ok := ald.Children()["Test"]; !ok {
		t.Error("Bob's dir should contain 'Test'")
	}

	if _, ok := byAuthor.Children()["Alice"]; ok {
		t.Error("Alice's dir should be pruned after author change")
	}
}

func TestRegistryEditStatusChangesReaderView(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author1")
	book.EpubPath = "Test.epub"
	book.Meta.Status = "unread"
	lib := libfake.Lib{
		EditFn: func(id int64, e model.Edits) (*library.Book, error) {
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
	if n := len(readerDir.Children()); n != 0 {
		t.Fatalf("expected 0 reader children for unread book, got %d", n)
	}

	// Change status to "reading" via the field file.
	bd := allBooks.Children()["Test"].(fs.Dir)
	writeField(t, bd, "status", "reading")

	// Now the reader view should reflect the change.
	ad, ok := readerDir.Children()["Author1"]
	if !ok {
		t.Fatal("reader should have 'Author1' subdir for a 'reading' status book")
	}
	ald := ad.(fs.ModDir)
	if _, ok := ald.Children()["Test.epub"]; !ok {
		t.Error("Author1's reader dir should contain 'Test.epub'")
	}
}

// Registry-internal behavior — edit on an unknown id, and the concurrent
// snapshot swap — is tested white-box in fs/registry (those tests call the
// unexported edit method). The tests here drive edits through the public 9P
// field-file path and assert the resulting rehoming across the real views.
