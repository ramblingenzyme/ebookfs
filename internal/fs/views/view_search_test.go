package views

import (
	"testing"
	"time"
)

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
