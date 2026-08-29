package registry

import (
	"errors"
	"github.com/ramblingenzyme/ebookfs/library"
	"slices"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// fakeView is a minimal BookView; the registry only needs a registered view to
// exercise the remove/add bracketing of a commit. It records nothing — the
// concurrency assertions read the BookDir snapshot directly.
type fakeView struct{}

func (fakeView) Add(*book.BookDir)    {}
func (fakeView) Remove(*book.BookDir) {}

func TestEditUnknownID(t *testing.T) {
	reg := NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{})

	status := "read"
	if err := reg.Edit(999, model.Edits{Status: &status}); err == nil {
		t.Fatal("expected error editing unknown book")
	}
}

// TestEditConcurrentSnapshotSwap exercises the snapshot swap under the
// concurrency go9p produces: handler goroutines read a BookDir's Stat/Children
// with no registry lock while edit commits swap the snapshot under r.mu. Run
// with -race to verify; without it the test still asserts a reader never
// observes a torn snapshot (a name that is neither the old nor the new title).
func TestEditConcurrentSnapshotSwap(t *testing.T) {
	// current mimics the library's authoritative state; EditFn runs under the
	// registry mutex, so reading and replacing it is serialized.
	current := testutil.MakeMutableBook(1, "Title A", "Alice")
	lib := libfake.Lib{
		EditFn: func(id int64, e model.Edits) (*library.Book, error) {
			updated := *current
			if e.Title != nil {
				updated.Title = *e.Title
			}
			current = &updated
			return testutil.WrapBook(&updated), nil
		},
	}
	reg := NewBookRegistry(testutil.NewTestFS(t), lib)
	reg.AddView(fakeView{})
	reg.Add(testutil.WrapBook(current))

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
			if err := reg.Edit(1, model.Edits{Title: &title}); err != nil {
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

// recordingView logs the Add/Remove notifications it receives, so tests can
// assert the registry's fan-out rather than inferring it from a view's state.
type recordingView struct {
	added   []int64
	removed []int64
}

func (v *recordingView) Add(d *book.BookDir)    { v.added = append(v.added, d.Book().ID()) }
func (v *recordingView) Remove(d *book.BookDir) { v.removed = append(v.removed, d.Book().ID()) }

func newTestRegistry(t *testing.T, lib libfake.Lib) (*BookRegistry, *recordingView) {
	t.Helper()
	reg := NewBookRegistry(testutil.NewTestFS(t), lib)
	v := &recordingView{}
	reg.AddView(v)
	return reg, v
}

func TestFSReturnsTheServingFilesystem(t *testing.T) {
	f := testutil.NewTestFS(t)
	reg := NewBookRegistry(f, libfake.Lib{})

	if reg.FS() != f {
		t.Error("FS() returned a different filesystem than the registry was built on")
	}
}

func TestAddNotifiesEveryView(t *testing.T) {
	reg, v := newTestRegistry(t, libfake.Lib{})

	reg.Add(testutil.MakeBook(1, "First", "Alice"))
	reg.Add(testutil.MakeBook(2, "Second", "Bob"))

	if !slices.Equal(v.added, []int64{1, 2}) {
		t.Errorf("view saw adds %v, want [1 2]", v.added)
	}
}

// TestAddSameIDReusesTheBookDir pins that a re-add keeps the same BookDir. Open
// 9P fids point at it, so replacing the object would strand every open handle.
func TestAddSameIDReusesTheBookDir(t *testing.T) {
	reg, _ := newTestRegistry(t, libfake.Lib{})

	reg.Add(testutil.MakeBook(1, "First", "Alice"))
	first := reg.books[1]
	reg.Add(testutil.MakeBook(1, "First", "Alice"))

	if reg.books[1] != first {
		t.Error("re-adding an id built a new BookDir, want the existing one reused")
	}
}

func TestRemove(t *testing.T) {
	t.Run("notifies views and forgets the book", func(t *testing.T) {
		reg, v := newTestRegistry(t, libfake.Lib{})
		reg.Add(testutil.MakeBook(1, "Doomed", "Alice"))

		reg.Remove(1)

		if !slices.Equal(v.removed, []int64{1}) {
			t.Errorf("view saw removes %v, want [1]", v.removed)
		}
		if _, ok := reg.books[1]; ok {
			t.Error("registry still holds the book after Remove")
		}
	})

	t.Run("unknown id is a no-op", func(t *testing.T) {
		reg, v := newTestRegistry(t, libfake.Lib{})
		reg.Add(testutil.MakeBook(1, "Kept", "Alice"))

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
	reg, v := newTestRegistry(t, libfake.Lib{})
	reg.Add(testutil.MakeBook(1, "Before", "Alice"))

	reg.RemoveView(v)
	reg.Add(testutil.MakeBook(2, "After", "Bob"))
	reg.Remove(1)

	if !slices.Equal(v.added, []int64{1}) {
		t.Errorf("view saw adds %v after being removed, want only the pre-removal [1]", v.added)
	}
	if len(v.removed) != 0 {
		t.Errorf("view saw removes %v after being removed, want none", v.removed)
	}
}

// TestResyncViewReplaysEveryBook covers the primitive the search directory is
// built on: a view that attaches after books exist — or changes its filter —
// converges on the registry's current state. reset runs first, then every
// registered book is offered, all under the registry lock.
func TestResyncViewReplaysEveryBook(t *testing.T) {
	reg, _ := newTestRegistry(t, libfake.Lib{})
	reg.Add(testutil.MakeBook(1, "First", "Alice"))
	reg.Add(testutil.MakeBook(2, "Second", "Bob"))

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
		current := testutil.MakeMutableBook(1, "Old Title", "Alice")
		lib := libfake.Lib{
			EditFn: func(_ int64, e model.Edits) (*library.Book, error) {
				updated := *current
				updated.Title = *e.Title
				return testutil.WrapBook(&updated), nil
			},
		}
		reg, v := newTestRegistry(t, lib)
		reg.Add(testutil.WrapBook(current))

		if err := reg.Edit(1, model.Edits{Title: new("New Title")}); err != nil {
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
		reg, _ := newTestRegistry(t, libfake.Lib{})

		if err := reg.Edit(999, model.Edits{Status: new("read")}); err == nil {
			t.Error("Edit on an unknown id returned nil, want an error")
		}
	})

	t.Run("library failure leaves the tree untouched", func(t *testing.T) {
		lib := libfake.Lib{
			EditFn: func(int64, model.Edits) (*library.Book, error) { return nil, errors.New("disk full") },
		}
		reg, v := newTestRegistry(t, lib)
		reg.Add(testutil.MakeBook(1, "Unchanged", "Alice"))

		if err := reg.Edit(1, model.Edits{Title: new("Never Written")}); err == nil {
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
