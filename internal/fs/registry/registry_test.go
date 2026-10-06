package registry

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestFSReturnsTheServingFilesystem(t *testing.T) {
	f := util.NewTestFS(t)
	reg := NewBookRegistry(f, mock.Editor{})

	if reg.FS() != f {
		t.Error("FS() returned a different filesystem than the registry was built on")
	}
}

func TestLoadNotifiesEveryView(t *testing.T) {
	reg, view := newTestRegistry(t)
	reg.Load(
		util.MakeBook(1, "First", "Alice"),
		util.MakeBook(2, "Second", "Bob"),
	)

	if !slices.Equal(view.added, []int64{1, 2}) {
		t.Errorf("view saw adds %v, want [1 2]", view.added)
	}
}

func TestLoadSameIDReusesTheBookDir(t *testing.T) {
	reg, _ := newTestRegistry(t)
	firstBook := util.MakeBook(1, "First", "Alice")
	reg.Load(firstBook)
	firstDir := reg.books[1]
	reg.Load(util.MakeBook(1, "First", "Alice"))

	if reg.books[1] != firstDir {
		t.Error("loading an existing id built a new BookDir, want the existing one reused")
	}
}

func TestAddViewReplaysRegisteredBooks(t *testing.T) {
	reg := NewBookRegistry(util.NewTestFS(t), mock.Editor{})
	reg.Load(
		util.MakeBook(1, "First", "Alice"),
		util.MakeBook(2, "Second", "Bob"),
	)

	lateView := &recordingView{}
	reg.AddView(lateView)

	got := slices.Clone(lateView.added)
	slices.Sort(got)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("late view saw %v, want both registered books", got)
	}
}

func TestRemoveViewStopsNotifications(t *testing.T) {
	reg, view := newTestRegistry(t)
	reg.Load(util.MakeBook(1, "Before", "Alice"))

	reg.RemoveView(view)
	reg.Load(util.MakeBook(2, "After", "Bob"))

	if !slices.Equal(view.added, []int64{1}) {
		t.Errorf("view saw adds %v after removal, want only [1]", view.added)
	}
	if len(view.removed) != 0 {
		t.Errorf("view saw removes %v after removal, want none", view.removed)
	}
}

func TestResyncViewResetsThenReplaysBooks(t *testing.T) {
	reg, _ := newTestRegistry(t)
	reg.Load(
		util.MakeBook(1, "First", "Alice"),
		util.MakeBook(2, "Second", "Bob"),
	)

	lateView := &recordingView{}
	var resetRan bool
	reg.ResyncView(lateView, func() {
		if len(lateView.added) != 0 {
			t.Error("reset ran after books were replayed, want it first")
		}
		resetRan = true
	})

	if !resetRan {
		t.Fatal("reset callback was not called")
	}
	got := slices.Clone(lateView.added)
	slices.Sort(got)
	if !slices.Equal(got, []int64{1, 2}) {
		t.Errorf("resynced view saw %v, want both registered books", got)
	}
}

type recordingView struct {
	added   []int64
	removed []int64
}

func (v *recordingView) Add(dir *book.BookDir) {
	v.added = append(v.added, dir.Book().ID())
}

func (v *recordingView) Remove(dir *book.BookDir) {
	v.removed = append(v.removed, dir.Book().ID())
}

func newTestRegistry(t *testing.T) (*BookRegistry, *recordingView) {
	t.Helper()
	reg := NewBookRegistry(util.NewTestFS(t), mock.Editor{})
	view := &recordingView{}
	reg.AddView(view)
	return reg, view
}
