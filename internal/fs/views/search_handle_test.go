package views

import (
	"fmt"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
	"sync"
	"testing"
)

func TestSearchHandleResyncRebuildsMembership(t *testing.T) {
	reg, handle := newTestSearchHandle(t)

	b1 := makeBook(1, "Foundation", "Isaac Asimov")
	b1.Meta.Tags = []string{"sci-fi"}
	b2 := makeBook(2, "The Hobbit", "J.R.R. Tolkien")
	b2.Meta.Tags = []string{"fantasy"}
	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	// Before any query the results dir lists nothing.
	fstest.ChildCount(t, handle.results, 0)

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	fstest.HasChild(t, handle.results, "Foundation")
	fstest.ChildCount(t, handle.results, 1)
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want %q", got, "tag:sci-fi")
	}

	// A matching book ingested after the query appears live.
	b3 := makeBook(3, "Dune", "Frank Herbert")
	b3.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b3))
	fstest.HasChild(t, handle.results, "Dune")

	// Requerying rebuilds membership from scratch.
	handle.executeSearch(library.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
	fstest.HasChild(t, handle.results, "The Hobbit")
	fstest.ChildCount(t, handle.results, 1)
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
	fstest.ChildCount(t, handle.results, perTag)
}

func TestSearchCtlReadsCommittedQuery(t *testing.T) {
	_, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	readCtl := func(num uint64) string {
		t.Helper()
		return fstest.Fid(t, ctl, num).Get(proto.Oread, 4096)
	}

	if got := readCtl(1); got != "" {
		t.Errorf("ctl read = %q on a handle that has run no query, want empty", got)
	}

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if got := readCtl(2); got != "tag:sci-fi" {
		t.Errorf("ctl read = %q, want the committed query %q", got, "tag:sci-fi")
	}

	// A fid opened before a requery keeps reporting its snapshot.
	early := fstest.Fid(t, ctl, 3)
	early.Open(proto.Oread)
	handle.executeSearch(library.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
	if data := early.Read(0, 4096); data != "tag:sci-fi" {
		t.Errorf("ctl read on a fid opened before the requery = %q, want its snapshot %q", data, "tag:sci-fi")
	}
	if got := readCtl(4); got != "tag:fantasy" {
		t.Errorf("ctl read on a fid opened after the requery = %q, want %q", got, "tag:fantasy")
	}
}

// The buffered-write contract: the query is accumulated by Write and only run at
// clunk, so a client that writes in several chunks gets one search rather than a
// partial one per write.
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
	fid := fstest.Fid(t, ctl, 7)
	first, rest := "tag:sci-fi", "+title:Foundation"
	fid.Write(0, first)
	fid.Write(uint64(len(first)), rest)

	// The query is deferred until the fid closes.
	fstest.ChildCount(t, handle.results, 0)
	if got := handle.currentQueryText(); got != "" {
		t.Errorf("currentQueryText = %q before clunk, want no query committed yet", got)
	}
	query := first + rest

	fid.Close()
	fstest.HasChild(t, handle.results, "Foundation")
	fstest.ChildCount(t, handle.results, 1)
	if got := handle.currentQueryText(); got != query {
		t.Errorf("currentQueryText = %q, want %q — ctl reads back the committed query", got, query)
	}
}

// The error surfaces at clunk and leaves the previous results standing, rather
// than clearing them to nothing.
func TestSearchCtlRejectsUnparseableQuery(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b := makeBook(1, "Foundation", "Isaac Asimov")
	b.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")

	fstest.Fid(t, ctl, 3).Write(0, "publisher:Tor")
	if err := ctl.Close(3); err == nil {
		t.Error("Close on an unparseable query returned nil, want the parse error surfaced to the client")
	}

	// The rejected query must leave the previous results standing.
	fstest.HasChild(t, handle.results, "Foundation")
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want the last accepted query %q", got, "tag:sci-fi")
	}
}

// The two no-op paths: a fid clunked without ever writing, and one that wrote
// only whitespace. Neither may be treated as a query, or opening ctl to read it
// would wipe the results.
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
			fid := fstest.Fid(t, ctl, tc.fid)
			if tc.data != "" {
				fid.Write(0, tc.data)
			}
			fid.Close()
			fstest.HasChild(t, handle.results, "Foundation")
		})
	}
}

// The client-driven release path. Dropping the directory is only part of it:
// the results dir must also come off the registry, or a handle no client can
// reach goes on receiving every book event for the life of the process.
func TestSearchCtlCloseTearsDownHandle(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	b := makeBook(1, "Foundation", "Isaac Asimov")
	b.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(b))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	fstest.ChildCount(t, handle.results, 1)

	closer := fstest.Fid(t, ctl, 5)
	closer.Write(0, "close\n")
	closer.Close()

	if hasHandleDir(sd, handle.id) {
		t.Errorf(`handle %d still under search/ after writing "close", children: %v`, handle.id, fstest.ChildNames(sd))
	}

	later := makeBook(2, "Dune", "Frank Herbert")
	later.Meta.Tags = []string{"sci-fi"}
	reg.Add(wrapBook(later))
	fstest.NoChild(t, handle.results, "Dune")

	// A client that clunks a second ctl fid carrying "close" must not take a
	// second teardown pass over an id that is already gone.
	again := fstest.Fid(t, ctl, 6)
	again.Write(0, "close")
	again.Close()
}
