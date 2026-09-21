package registry

import (
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
)

func TestEditUnknownID(t *testing.T) {
	reg := NewBookRegistry(util.NewTestFS(t), mock.Editor{})

	status := "read"
	if err := reg.Edit(999, library.Edits{Status: &status}); err == nil {
		t.Fatal("expected error editing unknown book")
	}
}

// The snapshot swap under the concurrency go9p produces: handler goroutines read
// a BookDir's Stat/Children with no registry lock while edit commits swap the
// snapshot under r.mu. Run with -race to verify; without it the test still
// asserts a reader never observes a torn snapshot (a name that is neither the
// old nor the new title).
func TestEditConcurrentSnapshotSwap(t *testing.T) {
	// current mimics the library's authoritative state; EditFn runs under the
	// registry mutex, so reading and replacing it is serialized.
	current := util.MakeMutableBook(1, "Title A", "Alice")
	lib := mock.Editor{
		EditFn: func(id int64, e library.Edits) (*library.Book, error) {
			updated := *current
			if e.Title != nil {
				updated.Title = *e.Title
			}
			current = &updated
			return util.WrapBook(&updated), nil
		},
	}
	reg := NewBookRegistry(util.NewTestFS(t), lib)
	reg.AddView(fakeView{})
	reg.Add(util.WrapBook(current))

	// White-box: reach the stable BookDir the registry created.
	bd := reg.books[1]
	titleFF := bd.Children()["title"]

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Go(func() {
		defer close(done)
		titles := [2]string{"Title B", "Title A"}
		for i := range 200 {
			title := titles[i%2]
			if err := reg.Edit(1, library.Edits{Title: &title}); err != nil {
				t.Errorf("edit: %v", err)
				return
			}
		}
	})

	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				if name := bd.Stat().Name; name != "Title A" && name != "Title B" {
					t.Errorf("torn BookDir name: %q", name)
					return
				}
				titleFF.Stat() // field get closure reads the snapshot
			}
		})
	}
	wg.Wait()
}

func TestFSReturnsTheServingFilesystem(t *testing.T) {
	f := util.NewTestFS(t)
	reg := NewBookRegistry(f, mock.Editor{})

	if reg.FS() != f {
		t.Error("FS() returned a different filesystem than the registry was built on")
	}
}

func TestAddNotifiesEveryView(t *testing.T) {
	reg, v := newTestRegistry(t, mock.Editor{})

	reg.Add(util.MakeBook(1, "First", "Alice"))
	reg.Add(util.MakeBook(2, "Second", "Bob"))

	if !slices.Equal(v.added, []int64{1, 2}) {
		t.Errorf("view saw adds %v, want [1 2]", v.added)
	}
}

// A re-add keeps the same BookDir. Open 9P fids point at it, so replacing the
// object would strand every open handle.
func TestAddSameIDReusesTheBookDir(t *testing.T) {
	reg, _ := newTestRegistry(t, mock.Editor{})

	reg.Add(util.MakeBook(1, "First", "Alice"))
	first := reg.books[1]
	reg.Add(util.MakeBook(1, "First", "Alice"))

	if reg.books[1] != first {
		t.Error("re-adding an id built a new BookDir, want the existing one reused")
	}
}

func TestRemove(t *testing.T) {
	t.Run("notifies views and forgets the book", func(t *testing.T) {
		reg, v := newTestRegistry(t, mock.Editor{})
		reg.Add(util.MakeBook(1, "Doomed", "Alice"))

		reg.Remove(1)

		if !slices.Equal(v.removed, []int64{1}) {
			t.Errorf("view saw removes %v, want [1]", v.removed)
		}
		if _, ok := reg.books[1]; ok {
			t.Error("registry still holds the book after Remove")
		}
	})

	t.Run("unknown id is a no-op", func(t *testing.T) {
		reg, v := newTestRegistry(t, mock.Editor{})
		reg.Add(util.MakeBook(1, "Kept", "Alice"))

		reg.Remove(999)

		if len(v.removed) != 0 {
			t.Errorf("view saw removes %v for an id that was never added, want none", v.removed)
		}
		if _, ok := reg.books[1]; !ok {
			t.Error("Remove of an unknown id dropped a registered book")
		}
	})
}

func TestRemoveViewStopsNotifications(t *testing.T) {
	reg, v := newTestRegistry(t, mock.Editor{})
	reg.Add(util.MakeBook(1, "Before", "Alice"))

	reg.RemoveView(v)
	reg.Add(util.MakeBook(2, "After", "Bob"))
	reg.Remove(1)

	if !slices.Equal(v.added, []int64{1}) {
		t.Errorf("view saw adds %v after being removed, want only the pre-removal [1]", v.added)
	}
	if len(v.removed) != 0 {
		t.Errorf("view saw removes %v after being removed, want none", v.removed)
	}
}

// The primitive the search directory is built on. A view attaching after books
// exist, or changing its filter, converges on the registry's current state.
func TestResyncViewReplaysEveryBook(t *testing.T) {
	reg, _ := newTestRegistry(t, mock.Editor{})
	reg.Add(util.MakeBook(1, "First", "Alice"))
	reg.Add(util.MakeBook(2, "Second", "Bob"))

	late := &recordingView{}
	var resetRan bool
	reg.ResyncView(late, func() {
		if len(late.added) != 0 {
			t.Error("reset ran after books were replayed, want it first")
		}
		resetRan = true
	})

	if !resetRan {
		t.Error("reset was never called")
	}
	got := slices.Clone(late.added)
	slices.Sort(got)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("resynced view saw %v, want every registered book [1 2]", got)
	}
}

func TestEdit(t *testing.T) {
	t.Run("persists and rehomes the book", func(t *testing.T) {
		current := util.MakeMutableBook(1, "Old Title", "Alice")
		lib := mock.Editor{
			EditFn: func(_ int64, e library.Edits) (*library.Book, error) {
				updated := *current
				updated.Title = *e.Title
				return util.WrapBook(&updated), nil
			},
		}
		reg, v := newTestRegistry(t, lib)
		reg.Add(util.WrapBook(current))

		if err := reg.Edit(1, library.Edits{Title: new("New Title")}); err != nil {
			t.Fatalf("Edit: %v", err)
		}

		// The commit brackets the snapshot swap with remove/add so views refile
		// the book under its new name.
		if !slices.Equal(v.removed, []int64{1}) || !slices.Equal(v.added, []int64{1, 1}) {
			t.Errorf("view saw adds %v / removes %v, want the edit bracketed by one remove and one re-add", v.added, v.removed)
		}
		if got := reg.books[1].Book().Title(); got != "New Title" {
			t.Errorf("BookDir snapshot title = %q, want the edited title", got)
		}
	})

	t.Run("unknown id", func(t *testing.T) {
		reg, _ := newTestRegistry(t, mock.Editor{})

		if err := reg.Edit(999, library.Edits{Status: new("read")}); err == nil {
			t.Error("Edit on an unknown id returned nil, want an error")
		}
	})

	t.Run("library failure leaves the tree untouched", func(t *testing.T) {
		lib := mock.Editor{
			EditFn: func(int64, library.Edits) (*library.Book, error) { return nil, errors.New("disk full") },
		}
		reg, v := newTestRegistry(t, lib)
		reg.Add(util.MakeBook(1, "Unchanged", "Alice"))

		if err := reg.Edit(1, library.Edits{Title: new("Never Written")}); err == nil {
			t.Fatal("Edit returned nil despite the library failing")
		}

		if len(v.removed) != 0 {
			t.Errorf("view saw removes %v after a failed edit, want the tree untouched", v.removed)
		}
		if got := reg.books[1].Book().Title(); got != "Unchanged" {
			t.Errorf("snapshot title = %q, want it unchanged after a failed edit", got)
		}
	})
}

// A commit must bracket the swap as Remove, then swap, then Add, because Add
// and Remove read the book's current state. Remove has to see the old title or
// it deletes the wrong entry and leaves a ghost in the 9P tree; Add has to see
// the new one or it files the book under its old name.
//
// The bracketing test that records ids cannot catch this: ids are identical
// either way, so swapping before the Remove would leave it passing while every
// view broke.
func TestCommitShowsOldStateToRemoveAndNewStateToAdd(t *testing.T) {
	current := util.MakeMutableBook(1, "Old Title", "Alice")
	lib := mock.Editor{
		EditFn: func(_ int64, e library.Edits) (*library.Book, error) {
			updated := *current
			updated.Title = *e.Title
			return util.WrapBook(&updated), nil
		},
	}
	reg := NewBookRegistry(util.NewTestFS(t), lib)
	v := &snapshotView{}
	reg.AddView(v)
	reg.Add(util.WrapBook(current))

	if err := reg.Edit(1, library.Edits{Title: new("New Title")}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if !slices.Equal(v.removedTitles, []string{"Old Title"}) {
		t.Errorf("Remove saw %v, want the pre-edit title; a view removing by the new name misses its entry", v.removedTitles)
	}
	// The first Add is the initial registration, the second is the re-file.
	if !slices.Equal(v.addedTitles, []string{"Old Title", "New Title"}) {
		t.Errorf("Add saw %v, want the re-file to carry the post-edit title", v.addedTitles)
	}
}

type fakeView struct{}

func (fakeView) Add(*book.BookDir)    {}
func (fakeView) Remove(*book.BookDir) {}

type recordingView struct {
	added   []int64
	removed []int64
}

func (v *recordingView) Add(d *book.BookDir)    { v.added = append(v.added, d.Book().ID()) }
func (v *recordingView) Remove(d *book.BookDir) { v.removed = append(v.removed, d.Book().ID()) }

func newTestRegistry(t *testing.T, lib Editor) (*BookRegistry, *recordingView) {
	t.Helper()
	reg := NewBookRegistry(util.NewTestFS(t), lib)
	v := &recordingView{}
	reg.AddView(v)
	return reg, v
}

// snapshotView records the title each callback observed. Commit order is one
// side of the contract, which snapshot each side reads is the other.
type snapshotView struct {
	addedTitles   []string
	removedTitles []string
}

func (v *snapshotView) Add(d *book.BookDir) {
	v.addedTitles = append(v.addedTitles, d.Book().Title())
}

func (v *snapshotView) Remove(d *book.BookDir) {
	v.removedTitles = append(v.removedTitles, d.Book().Title())
}
