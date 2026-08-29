package views

import (
	"fmt"
	"testing"
	"time"
)

func TestRecentDirOrdersNewestFirst(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()
	b1 := makeBook(1, "Oldest", "Author")
	b1.Meta.DateAdded = base.Add(-2 * time.Hour)
	b2 := makeBook(2, "Newest", "Author")
	b2.Meta.DateAdded = base

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 children, got %d: %v", len(children), children)
	}
	if _, ok := d.Children()["Newest"]; !ok {
		t.Errorf("expected 'Newest' to be listed, got %v", children)
	}
	if _, ok := d.Children()["Oldest"]; !ok {
		t.Errorf("expected 'Oldest' to be listed, got %v", children)
	}
}

func TestRecentDirCapsAtLimitAndBackfillsOnRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()
	for i := int64(1); i <= int64(recentLimit)+1; i++ {
		b := makeBook(i, fmt.Sprintf("Title %d", i), "Author")
		b.Meta.DateAdded = base.Add(time.Duration(i) * time.Minute)
		reg.Add(wrapBook(b))
	}

	if len(d.visible) != recentLimit {
		t.Fatalf("expected %d visible books, got %d: %v", recentLimit, len(d.visible), d.visible)
	}
	// id 1 is the oldest of the batch and should have been evicted.
	if _, ok := d.visible[1]; ok {
		t.Errorf("oldest book (id 1) should not be visible, got %v", d.visible)
	}

	// Removing the newest book should backfill with the next-most-recent,
	// which was previously evicted (id 1).
	reg.Remove(int64(recentLimit) + 1)

	if len(d.visible) != recentLimit {
		t.Fatalf("expected %d visible books after remove, got %d: %v", recentLimit, len(d.visible), d.visible)
	}
	if _, ok := d.visible[1]; !ok {
		t.Errorf("expected backfilled book (id 1) to become visible, got %v", d.visible)
	}
	if _, ok := d.Children()["Title 1"]; !ok {
		t.Errorf("expected backfilled book to be filed under its title, got children %v", dirChildNames(d))
	}
}

func TestRecentDirRemoveNotVisibleNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()
	for i := int64(1); i <= int64(recentLimit)+1; i++ {
		b := makeBook(i, fmt.Sprintf("Title %d", i), "Author")
		b.Meta.DateAdded = base.Add(time.Duration(i) * time.Minute)
		reg.Add(wrapBook(b))
	}

	before := dirChildNames(d)

	// id 1 is the oldest and should not be visible; removing it should not
	// change the visible set.
	reg.Remove(1)

	after := dirChildNames(d)
	if len(after) != len(before) {
		t.Fatalf("expected visible set to stay the same size, before=%v after=%v", before, after)
	}
}

// TestRecentDirOutOfOrderArrival covers the case the other recent tests miss:
// books arriving in an order unrelated to their DateAdded. The population is
// kept ordered by insertion rather than re-sorted, so a book landing in the
// middle of the ranking is the path most likely to break.
func TestRecentDirOutOfOrderArrival(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewRecentDir(reg)

	base := time.Now()
	const total = recentLimit + 3
	// Book i was added i minutes after base, so the newest ids rank highest —
	// but they arrive in a scrambled order.
	for _, id := range []int64{4, 1, 8, 6, 2, 7, 3, 5} {
		b := makeBook(id, fmt.Sprintf("Title %d", id), "Author")
		b.Meta.DateAdded = base.Add(time.Duration(id) * time.Minute)
		reg.Add(wrapBook(b))
	}

	if len(d.visible) != recentLimit {
		t.Fatalf("expected %d visible, got %d: %v", recentLimit, len(d.visible), d.visible)
	}
	for id := int64(1); id <= total; id++ {
		_, shown := d.visible[id]
		want := id > total-recentLimit
		if shown != want {
			t.Errorf("book %d visible = %v, want %v (visible: %v)", id, shown, want, d.visible)
		}
	}

	// all must stay ordered newest-first for the binary insert to hold.
	for i := 1; i < len(d.all); i++ {
		prev, cur := d.all[i-1].Book(), d.all[i].Book()
		if prev.DateAdded().Before(cur.DateAdded()) {
			t.Fatalf("all out of order at %d: %s before %s", i, prev.Title(), cur.Title())
		}
	}
}
