package views

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
)

func TestRecentDirCapsAtLimitAndBackfillsOnRemove(t *testing.T) {
	reg, d := overfilledRecentDir(t)
	wantRecent(t, d, 2, 3, 4, 5, 6)

	// Removing the newest book backfills the slot with the evicted oldest.
	removeBookFromView(t, reg, d, int64(recentLimit)+1)
	wantRecent(t, d, 1, 2, 3, 4, 5)

	// The oldest is not shown, so removing it changes nothing.
	reg, d = overfilledRecentDir(t)
	removeBookFromView(t, reg, d, 1)
	wantRecent(t, d, 2, 3, 4, 5, 6)
}

// Books arriving in an order unrelated to their DateAdded. Add inserts into a
// ranked population rather than re-sorting, so a book landing in the middle of
// the ranking is the path most likely to break.
func TestRecentDirOutOfOrderArrival(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()
	// Book i was added i minutes after base, so the newest ids rank highest, but
	// they arrive in a scrambled order.
	for _, id := range []int64{4, 1, 8, 6, 2, 7, 3, 5} {
		b := makeBook(id, fmt.Sprintf("Title %d", id), "Author")
		b.Meta.DateAdded = base.Add(time.Duration(id) * time.Minute)
		reg.Load(wrapBook(b))
	}

	wantRecent(t, d, 4, 5, 6, 7, 8)
}

func wantRecent(t *testing.T, d *recentDir, ids ...int64) {
	t.Helper()

	var want []string
	for _, id := range ids {
		want = append(want, fmt.Sprintf("Title %d", id))
	}

	if got := fstest.ChildNames(d); !slices.Equal(got, want) {
		t.Errorf("recent/ lists %v, want %v", got, want)
	}
}

// overfilledRecentDir files one book more than recentLimit, staggered a minute
// apart so "Title 1" is the oldest and the only one evicted. The registry comes
// back too, since these tests drive the view through it rather than directly.
func overfilledRecentDir(t *testing.T) (*registry.BookRegistry, *recentDir) {
	t.Helper()
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()

	for i := int64(1); i <= int64(recentLimit)+1; i++ {
		b := makeBook(i, fmt.Sprintf("Title %d", i), "Author")
		b.Meta.DateAdded = base.Add(time.Duration(i) * time.Minute)
		reg.Load(wrapBook(b))
	}

	return reg, d
}
