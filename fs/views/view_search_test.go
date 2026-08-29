package views

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// newTestSearchDir builds a search directory over a fresh in-memory FS with no
// backing library. ttl and maxHandles are passed through so the reclamation
// tests can drive cleanupLocked; a non-zero one starts the cleanup goroutine,
// hence the Close.
func newTestSearchDir(t *testing.T, ttl time.Duration, maxHandles int) (*registry.BookRegistry, *searchDir) {
	t.Helper()
	f := newTestFS(t)
	reg := registry.NewBookRegistry(f, nil)
	sd := NewSearchDir(f, reg, ttl, maxHandles)
	t.Cleanup(sd.Close)
	return reg, sd
}

func newTestSearchHandle(t *testing.T) (*registry.BookRegistry, *searchHandleDir) {
	t.Helper()
	reg, sd := newTestSearchDir(t, 0, 0)
	return reg, sd.allocateHandle()
}

// ctlOf returns a handle's ctl file, the only way in to the search protocol.
func ctlOf(t *testing.T, handle *searchHandleDir) *searchCtlFile {
	t.Helper()
	ctl, ok := handle.Children()["ctl"].(*searchCtlFile)
	if !ok {
		t.Fatalf("handle %d has no ctl file, children: %v", handle.id, dirChildNames(handle))
	}
	return ctl
}

func hasHandleDir(sd *searchDir, id int64) bool {
	_, ok := sd.Children()[strconv.FormatInt(id, 10)]
	return ok
}

func TestSearchHandleResyncRebuildsMembership(t *testing.T) {
	reg, handle := newTestSearchHandle(t)

	b1 := makeBook(1, "Foundation", "Isaac Asimov")
	b1.Meta.Tags = []string{"sci-fi"}
	b2 := makeBook(2, "The Hobbit", "J.R.R. Tolkien")
	b2.Meta.Tags = []string{"fantasy"}
	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	// Before any query the results dir lists nothing.
	if n := len(dirChildNames(handle.results)); n != 0 {
		t.Fatalf("expected empty results before query, got %d entries", n)
	}

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if _, ok := handle.results.Children()["Foundation"]; !ok {
		t.Errorf("expected Foundation in results, got %v", dirChildNames(handle.results))
	}
	if len(dirChildNames(handle.results)) != 1 {
		t.Errorf("expected 1 result, got %v", dirChildNames(handle.results))
	}
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want %q", got, "tag:sci-fi")
	}

	// A matching book ingested after the query appears live.
	b3 := makeBook(3, "Dune", "Frank Herbert")
	b3.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b3))
	if _, ok := handle.results.Children()["Dune"]; !ok {
		t.Errorf("expected live-added Dune in results, got %v", dirChildNames(handle.results))
	}

	// Requerying rebuilds membership from scratch.
	handle.executeSearch(library.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
	if _, ok := handle.results.Children()["The Hobbit"]; !ok {
		t.Errorf("expected The Hobbit in results, got %v", dirChildNames(handle.results))
	}
	if len(dirChildNames(handle.results)) != 1 {
		t.Errorf("expected 1 result after requery, got %v", dirChildNames(handle.results))
	}
}

// Regression test: executeSearch used to mutate the results listing and the
// handle's query metadata outside the registry lock, racing registry
// notifications (concurrent map writes) and cleanup/ctl reads. Run under
// -race; the final resync must also converge on exactly the matching set.
func TestSearchHandleConcurrentRequeryAndRegistryEvents(t *testing.T) {
	reg, handle := newTestSearchHandle(t)

	const perTag = 100
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := range 2 * perTag {
			if i%2 == 0 {
				handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
			} else {
				handle.executeSearch(library.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := range 2 * perTag {
			b := makeBook(int64(i+1), fmt.Sprintf("Book %d", i+1), "Author")
			if i%2 == 0 {
				b.Meta.Tags = []string{"sci-fi"}
			} else {
				b.Meta.Tags = []string{"fantasy"}
			}
			reg.Add(wrapBook(b))
		}
	}()
	go func() {
		defer wg.Done()
		for range 2 * perTag {
			_ = handle.currentQueryText()
			_ = handle.lastQuery()
		}
	}()
	wg.Wait()

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if got := len(dirChildNames(handle.results)); got != perTag {
		t.Errorf("expected %d sci-fi results after quiescent requery, got %d", perTag, got)
	}
}

// TestMakeMatchesFn covers the membership predicate field by field. It is the
// single authority a handle uses for both its resync and its live updates, so
// every field needs a matching and a non-matching case, and the AND across
// fields needs one where a single field fails while the rest hold.
func TestMakeMatchesFn(t *testing.T) {
	base := func() *library.Book {
		b := makeBook(7, "Foundation and Empire", "Isaac Asimov", "Ray Bradbury")
		b.Meta.Tags = []string{"sci-fi", "classic"}
		b.Meta.Status = "unread"
		b.Series = &model.SeriesRef{Name: "Foundation", Index: "2"}
		return wrapBook(b)
	}
	standalone := func() *library.Book {
		b := makeBook(7, "Foundation and Empire", "Isaac Asimov", "Ray Bradbury")
		b.Meta.Tags = []string{"sci-fi", "classic"}
		b.Meta.Status = "unread"
		b.Series = nil
		return wrapBook(b)
	}

	tests := []struct {
		name  string
		query library.Query
		book  func() *library.Book
		want  bool
	}{
		{"empty query matches anything", library.Query{}, base, true},

		{"author matches", library.Query{Authors: []string{"Isaac Asimov"}}, base, true},
		// Any one author of a multi-author book is enough.
		{"co-author matches", library.Query{Authors: []string{"Ray Bradbury"}}, base, true},
		{"author does not match", library.Query{Authors: []string{"J.R.R. Tolkien"}}, base, false},
		// Authors compare exactly — unlike titles, they are not a substring field.
		{"author is not a substring match", library.Query{Authors: []string{"Asimov"}}, base, false},

		{"tag matches", library.Query{Tags: []string{"sci-fi"}}, base, true},
		{"tag matches any of several", library.Query{Tags: []string{"fantasy", "classic"}}, base, true},
		{"tag does not match", library.Query{Tags: []string{"fantasy"}}, base, false},

		{"series matches", library.Query{Series: []string{"Foundation"}}, base, true},
		{"series does not match", library.Query{Series: []string{"Dune"}}, base, false},
		// A book in no series can never satisfy a series filter, and must not
		// panic on the nil.
		{"series filter rejects a standalone book", library.Query{Series: []string{"Foundation"}}, standalone, false},

		{"status matches", library.Query{Status: []string{"unread"}}, base, true},
		{"status does not match", library.Query{Status: []string{"read"}}, base, false},

		{"id matches", library.Query{IDs: []int64{7}}, base, true},
		{"id does not match", library.Query{IDs: []int64{8}}, base, false},

		// Titles are the one substring field, and case-insensitive on both sides.
		{"title substring matches", library.Query{Titles: []string{"and Empire"}}, base, true},
		{"title match ignores case", library.Query{Titles: []string{"FOUNDATION"}}, base, true},
		{"title does not match", library.Query{Titles: []string{"Hobbit"}}, base, false},
		// ExactTitles is ctl's setting, but the predicate honours it so this
		// path and Index.Search stay one semantics.
		{"exact title matches", library.Query{Titles: []string{"Foundation and Empire"}, ExactTitles: true}, base, true},
		{"exact title rejects a substring", library.Query{Titles: []string{"and Empire"}, ExactTitles: true}, base, false},

		// Across fields every populated one must hold...
		{
			"every field matching",
			library.Query{
				Authors: []string{"Isaac Asimov"},
				Tags:    []string{"sci-fi"},
				Series:  []string{"Foundation"},
				Status:  []string{"unread"},
				IDs:     []int64{7},
				Titles:  []string{"empire"},
			},
			base, true,
		},
		// ...so one failing field rejects the book even when the others match.
		{"one field of several fails", library.Query{Tags: []string{"sci-fi"}, Status: []string{"read"}}, base, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := makeMatchesFn(tc.query)(tc.book()); got != tc.want {
				t.Errorf("matches = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSearchCtlReadsCommittedQuery covers ctl's read side. The value is
// snapshotted at open like every other file in the tree, so a fid reports the
// query that was current when it opened rather than tracking later requeries.
func TestSearchCtlReadsCommittedQuery(t *testing.T) {
	_, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	readCtl := func(fid uint64) string {
		t.Helper()
		if err := ctl.Open(fid, proto.Oread); err != nil {
			t.Fatalf("Open fid %d: %v", fid, err)
		}
		data, err := ctl.Read(fid, 0, 4096)
		if err != nil {
			t.Fatalf("Read fid %d: %v", fid, err)
		}
		return string(data)
	}

	if got := readCtl(1); got != "" {
		t.Errorf("ctl read = %q on a handle that has run no query, want empty", got)
	}

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if got := readCtl(2); got != "tag:sci-fi" {
		t.Errorf("ctl read = %q, want the committed query %q", got, "tag:sci-fi")
	}

	// A fid opened before a requery keeps reporting its snapshot.
	if err := ctl.Open(3, proto.Oread); err != nil {
		t.Fatalf("Open: %v", err)
	}
	handle.executeSearch(library.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
	data, err := ctl.Read(3, 0, 4096)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "tag:sci-fi" {
		t.Errorf("ctl read on a fid opened before the requery = %q, want its snapshot %q", data, "tag:sci-fi")
	}
	if got := readCtl(4); got != "tag:fantasy" {
		t.Errorf("ctl read on a fid opened after the requery = %q, want %q", got, "tag:fantasy")
	}
}

// TestSearchCloneAllocatesHandlePerFid drives allocation the way a client does:
// every open of clone mints a handle, and reading the fid back reports which
// one. Clunking clone must not reclaim it — the handle outlives the fid that
// created it, and is released only by ctl or the cleanup sweep.
func TestSearchCloneAllocatesHandlePerFid(t *testing.T) {
	_, sd := newTestSearchDir(t, 0, 0)
	clone, ok := sd.Children()["clone"].(*cloneFile)
	if !ok {
		t.Fatalf("search dir has no clone file, children: %v", dirChildNames(sd))
	}

	// A fid that never opened has no handle to name.
	if _, err := clone.Read(1, 0, 32); err == nil {
		t.Error("Read on an unopened fid succeeded, want an error — no handle was ever allocated for it")
	}

	readID := func(fid uint64) string {
		t.Helper()
		if err := clone.Open(fid, proto.Oread); err != nil {
			t.Fatalf("Open fid %d: %v", fid, err)
		}
		data, err := clone.Read(fid, 0, 32)
		if err != nil {
			t.Fatalf("Read fid %d: %v", fid, err)
		}
		return string(data)
	}

	if got := readID(1); got != "1\n" {
		t.Errorf("first clone read = %q, want %q", got, "1\n")
	}
	if got := readID(2); got != "2\n" {
		t.Errorf("second clone read = %q, want %q — each open must mint its own handle", got, "2\n")
	}
	for _, id := range []int64{1, 2} {
		if !hasHandleDir(sd, id) {
			t.Errorf("no handle directory %d under search/, children: %v", id, dirChildNames(sd))
		}
	}

	if err := clone.Close(1); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !hasHandleDir(sd, 1) {
		t.Error("handle 1 disappeared when its clone fid was clunked, want it to persist until ctl or the sweep releases it")
	}
	if _, err := clone.Read(1, 0, 32); err == nil {
		t.Error("Read after clunk succeeded, want an error — the fid no longer names a handle")
	}
}

// TestSearchCtlExecutesQueryOnClunk pins the buffered-write contract: the query
// is accumulated by Write and only run at clunk, so a client that writes in
// several chunks gets one search rather than a partial one per write.
func TestSearchCtlExecutesQueryOnClunk(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b1 := makeBook(1, "Foundation", "Isaac Asimov")
	b1.Meta.Tags = []string{"sci-fi"}
	b2 := makeBook(2, "The Hobbit", "J.R.R. Tolkien")
	b2.Meta.Tags = []string{"fantasy"}
	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	// Split so the first chunk is a valid, matching query on its own: if the
	// write path executed anything, results would be populated before the clunk
	// rather than after it.
	const fid = 7
	first, rest := "tag:sci-fi", "+title:Foundation"
	if _, err := ctl.Write(fid, 0, []byte(first)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := ctl.Write(fid, uint64(len(first)), []byte(rest)); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n := len(dirChildNames(handle.results)); n != 0 {
		t.Errorf("results holds %d entries before clunk, want the query deferred until the fid closes", n)
	}
	if got := handle.currentQueryText(); got != "" {
		t.Errorf("currentQueryText = %q before clunk, want no query committed yet", got)
	}
	query := first + rest

	if err := ctl.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, ok := handle.results.Children()["Foundation"]; !ok {
		t.Errorf("Foundation missing from results after clunk, got %v", dirChildNames(handle.results))
	}
	if n := len(dirChildNames(handle.results)); n != 1 {
		t.Errorf("results = %v, want only the sci-fi book", dirChildNames(handle.results))
	}
	if got := handle.currentQueryText(); got != query {
		t.Errorf("currentQueryText = %q, want %q — ctl reads back the committed query", got, query)
	}
}

// TestSearchCtlRejectsUnparseableQuery checks the error surfaces at clunk and
// leaves the previous results standing, rather than clearing them to nothing.
func TestSearchCtlRejectsUnparseableQuery(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b := makeBook(1, "Foundation", "Isaac Asimov")
	b.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")

	if _, err := ctl.Write(3, 0, []byte("publisher:Tor")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := ctl.Close(3); err == nil {
		t.Error("Close on an unparseable query returned nil, want the parse error surfaced to the client")
	}
	if _, ok := handle.results.Children()["Foundation"]; !ok {
		t.Errorf("a rejected query cleared the previous results, got %v", dirChildNames(handle.results))
	}
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want the last accepted query %q", got, "tag:sci-fi")
	}
}

// TestSearchCtlIgnoresEmptyClunk covers the two no-op paths: a fid clunked
// without ever writing, and one that wrote only whitespace. Neither may be
// treated as a query, or opening ctl to read it would wipe the results.
func TestSearchCtlIgnoresEmptyClunk(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b := makeBook(1, "Foundation", "Isaac Asimov")
	b.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")

	tests := []struct {
		name string
		fid  uint64
		data string
	}{
		{"never wrote", 11, ""},
		{"whitespace only", 12, "  \n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.data != "" {
				if _, err := ctl.Write(tc.fid, 0, []byte(tc.data)); err != nil {
					t.Fatalf("Write: %v", err)
				}
			}
			if err := ctl.Close(tc.fid); err != nil {
				t.Fatalf("Close: %v", err)
			}
			if _, ok := handle.results.Children()["Foundation"]; !ok {
				t.Errorf("results cleared by a no-op clunk, got %v", dirChildNames(handle.results))
			}
		})
	}
}

// TestSearchCtlCloseTearsDownHandle covers the client-driven release path.
// Dropping the directory is only half of it: the results dir must also come off
// the registry, or a handle no client can reach goes on receiving every book
// event for the life of the process.
func TestSearchCtlCloseTearsDownHandle(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b := makeBook(1, "Foundation", "Isaac Asimov")
	b.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if n := len(dirChildNames(handle.results)); n != 1 {
		t.Fatalf("setup: results = %v, want the sci-fi book", dirChildNames(handle.results))
	}

	if _, err := ctl.Write(5, 0, []byte("close\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := ctl.Close(5); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if hasHandleDir(sd, handle.id) {
		t.Errorf(`handle %d still under search/ after writing "close", children: %v`, handle.id, dirChildNames(sd))
	}

	later := makeBook(2, "Dune", "Frank Herbert")
	later.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(later))
	if _, ok := handle.results.Children()["Dune"]; ok {
		t.Error("a book added after teardown reached the released handle's results — it was never unregistered from the registry")
	}

	// A client that clunks a second ctl fid carrying "close" must not take a
	// second teardown pass over an id that is already gone.
	if _, err := ctl.Write(6, 0, []byte("close")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := ctl.Close(6); err != nil {
		t.Errorf("second close: %v", err)
	}
}

// TestSearchDirEvictsHandlesPastTTL pins the idle sweep. Allocation runs it
// first, so a fresh open is enough to reclaim an abandoned handle — no waiting
// on the five-minute cleanup ticker.
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

// TestSearchDirEnforcesMaxHandles pins the cap itself. maxHandles is the only
// thing bounding how many live listings a client can pin in memory, each one
// registered for every book event, so a cap that admits one more than it says
// is a cap that cannot be trusted to hold anywhere.
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
