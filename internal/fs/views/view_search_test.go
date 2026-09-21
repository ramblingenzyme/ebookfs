package views

import (
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"testing"
	"time"
)

// Every open of clone mints a handle, and reading the fid back reports which
// one. Clunking clone must not reclaim it: the handle outlives the fid that
// created it, and is released only by ctl or the cleanup sweep.
func TestSearchCloneAllocatesHandlePerFid(t *testing.T) {
	_, sd := newTestSearchDir(t, 0, 0)
	clone := fstest.ChildAs[*cloneFile](t, sd, "clone")

	// A fid that never opened has no handle to name.
	if _, err := clone.Read(1, 0, 32); err == nil {
		t.Error("Read on an unopened fid succeeded, want an error — no handle was ever allocated for it")
	}

	readID := func(num uint64) string {
		t.Helper()
		return fstest.Fid(t, clone, num).Get(proto.Oread, 32)
	}

	if got := readID(1); got != "1\n" {
		t.Errorf("first clone read = %q, want %q", got, "1\n")
	}
	if got := readID(2); got != "2\n" {
		t.Errorf("second clone read = %q, want %q — each open must mint its own handle", got, "2\n")
	}
	for _, id := range []int64{1, 2} {
		if !hasHandleDir(sd, id) {
			t.Errorf("no handle directory %d under search/, children: %v", id, fstest.ChildNames(sd))
		}
	}

	first := fstest.Fid(t, clone, 1)
	first.Close()
	if !hasHandleDir(sd, 1) {
		t.Error("handle 1 disappeared when its clone fid was clunked, want it to persist until ctl or the sweep releases it")
	}
	first.WantReadError()
}

// The idle sweep. Allocation runs it first, so a fresh open is enough to
// reclaim an abandoned handle, with no waiting on the five-minute cleanup
// ticker.
func TestSearchDirEvictsHandlesPastTTL(t *testing.T) {
	_, sd := newTestSearchDir(t, time.Hour, 0)

	stale := sd.allocateHandle()
	stale.mu.Lock()
	stale.lastQueryTime = time.Now().Add(-2 * time.Hour)
	stale.mu.Unlock()

	fresh := sd.allocateHandle()

	if hasHandleDir(sd, stale.id) {
		t.Errorf("handle %d idle past the TTL survived the next allocation's sweep", stale.id)
	}
	if !hasHandleDir(sd, fresh.id) {
		t.Errorf("freshly allocated handle %d was swept", fresh.id)
	}
}

// maxHandles is the only thing bounding how many live listings a client can pin
// in memory, each one registered for every book event, so a cap that admits one
// more than it says is a cap that cannot be trusted to hold anywhere.
func TestSearchDirEnforcesMaxHandles(t *testing.T) {
	const maxHandles = 3
	_, sd := newTestSearchDir(t, 0, maxHandles)

	var ids []int64
	for i := range maxHandles + 2 {
		h := sd.allocateHandle()
		ids = append(ids, h.id)
		// Stamp a distinct, increasing time so eviction order is the LRU one
		// rather than whatever the clock's resolution happens to allow.
		h.mu.Lock()
		h.lastQueryTime = time.Unix(int64(1000+i), 0)
		h.mu.Unlock()
	}

	sd.mu.Lock()
	live := len(sd.handles)
	sd.mu.Unlock()
	if live > maxHandles {
		t.Errorf("live handles = %d after %d allocations, want at most maxHandles (%d)", live, len(ids), maxHandles)
	}

	evicted, kept := ids[:len(ids)-maxHandles], ids[len(ids)-maxHandles:]
	for _, id := range kept {
		if !hasHandleDir(sd, id) {
			t.Errorf("handle %d was evicted, want the %d most recently queried kept", id, maxHandles)
		}
	}
	for _, id := range evicted {
		if hasHandleDir(sd, id) {
			t.Errorf("handle %d survived, want the least recently queried evicted first", id)
		}
	}
}
