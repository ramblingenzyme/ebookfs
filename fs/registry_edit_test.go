package fs

import (
	"sync"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestRegistryEditTitleRehomesInAllViews(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Old Title", "Alice")
	book.Meta.Status = "unread"
	// The real library fetches the edit base by id; the fake closes over the
	// test's book instead.
	lib := fakeLib{
		editFn: func(id int64, e model.Edits) (*model.Book, error) {
			updated := *book
			if e.Title != nil {
				updated.Title = *e.Title
			}
			if e.SortTitle != nil {
				updated.SortTitle = *e.SortTitle
			}
			e.ApplyMeta(&updated.Meta)
			updated.Meta.DateModified = book.Meta.DateModified
			return &updated, nil
		},
	}
	reg := newBookRegistry(f, lib)

	allBooks := newAllBooksDir(reg)
	byAuthor := newByAuthorDir(reg)
	byID := newByIDDir(reg)

	reg.Add(book)

	// Find the title fieldFile in the bookDir and write to it.
	bd := allBooks.Children()["Old Title"].(*bookDir)
	titleFF := bd.Children()["title"].(*fieldFile)

	fid := uint64(1)
	if err := titleFF.Open(fid, proto.Otrunc); err != nil {
		t.Fatalf("Open title field: %v", err)
	}
	if _, err := titleFF.Write(fid, 0, []byte("New Title")); err != nil {
		t.Fatalf("Write title: %v", err)
	}
	if err := titleFF.Close(fid); err != nil {
		t.Fatalf("Close title: %v", err)
	}

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
	ald := ad.(*bookListDir)
	if _, ok := ald.Children()["New Title"]; !ok {
		t.Error("Alice's dir should contain 'New Title'")
	}
}

func TestRegistryEditAuthorsRehomesInByAuthor(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Alice")
	lib := fakeLib{
		editFn: func(id int64, e model.Edits) (*model.Book, error) {
			updated := *book
			if e.Authors != nil {
				updated.Authors = *e.Authors
			}
			e.ApplyMeta(&updated.Meta)
			updated.Meta.DateModified = book.Meta.DateModified
			return &updated, nil
		},
	}
	reg := newBookRegistry(f, lib)

	allBooks := newAllBooksDir(reg)
	byAuthor := newByAuthorDir(reg)

	reg.Add(book)

	// Change authors from Alice to Bob.
	bd := allBooks.Children()["Test"].(*bookDir)
	authorsFF := bd.Children()["authors"].(*fieldFile)

	fid := uint64(1)
	authorsFF.Open(fid, proto.Otrunc)
	authorsFF.Write(fid, 0, []byte("Bob"))
	authorsFF.Close(fid)

	// Author should now be Bob, Alice pruned.
	if _, ok := byAuthor.Children()["Bob"]; !ok {
		t.Error("by-author should have 'Bob'")
	}
	ad, ok := byAuthor.Children()["Bob"]
	if !ok {
		t.Fatal("by-author should have 'Bob'")
	}
	ald := ad.(*bookListDir)
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
	book.EpubFilename = "Test.epub"
	book.Meta.Status = "unread"
	lib := fakeLib{
		editFn: func(id int64, e model.Edits) (*model.Book, error) {
			updated := *book
			e.ApplyMeta(&updated.Meta)
			updated.Meta.DateModified = book.Meta.DateModified
			return &updated, nil
		},
	}
	reg := newBookRegistry(f, lib)
	allBooks := newAllBooksDir(reg)
	readerDir := newReaderDir(reg, testExporter{statuses: []string{"reading"}})

	reg.Add(book)

	// Reader view should not show the book when status is "unread".
	if n := len(readerDir.Children()); n != 0 {
		t.Fatalf("expected 0 reader children for unread book, got %d", n)
	}

	// Change status to "reading" via the field file.
	bd := allBooks.Children()["Test"].(*bookDir)
	statusFF := bd.Children()["status"].(*fieldFile)

	fid := uint64(1)
	statusFF.Open(fid, proto.Otrunc)
	statusFF.Write(fid, 0, []byte("reading"))
	statusFF.Close(fid)

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

// TestRegistryEditConcurrentReaders exercises the snapshot swap under the
// concurrency go9p actually produces: handler goroutines Stat/Children/read
// fields with no registry lock while edits commit. Run with -race to verify;
// without it the test still asserts a reader never observes a torn snapshot
// (a name that is neither the old nor the new title).
func TestRegistryEditConcurrentReaders(t *testing.T) {
	f := newTestFS(t)
	// current mimics the library's authoritative state; editFn runs under the
	// registry mutex, so reading and replacing it is serialized.
	current := makeBook(1, "Title A", "Alice")
	lib := fakeLib{
		editFn: func(id int64, e model.Edits) (*model.Book, error) {
			updated := *current
			if e.Title != nil {
				updated.Title = *e.Title
			}
			e.ApplyMeta(&updated.Meta)
			current = &updated
			return &updated, nil
		},
	}
	reg := newBookRegistry(f, lib)
	allBooks := newAllBooksDir(reg)
	byID := newByIDDir(reg)

	reg.Add(current)
	bd := allBooks.Children()["Title A"].(*bookDir)
	titleFF := bd.Children()["title"].(*fieldFile)

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		titles := [2]string{"Title B", "Title A"}
		for i := range 200 {
			title := titles[i%2]
			if err := reg.edit(1, model.Edits{Title: &title}); err != nil {
				t.Errorf("edit: %v", err)
				return
			}
		}
	}()

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if name := bd.Stat().Name; name != "Title A" && name != "Title B" {
					t.Errorf("torn bookDir name: %q", name)
					return
				}
				titleFF.Stat()  // field get closure reads the snapshot
				byID.Children() // namedBookDir.Stat recomputes the entry name
			}
		}()
	}
	wg.Wait()
}

func TestRegistryEditUnknownID(t *testing.T) {
	reg := newBookRegistry(newTestFS(t), fakeLib{})

	status := "read"
	err := reg.edit(999, model.Edits{Status: &status})
	if err == nil {
		t.Fatal("expected error editing unknown book")
	}
}
