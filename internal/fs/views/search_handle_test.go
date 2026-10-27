package views

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestSearchHandleResyncRebuildsMembership(t *testing.T) {
	reg, handle := newTestSearchHandle(t)

	reg.Load(makeBookWithTags(1, "Foundation", "Isaac Asimov", "sci-fi"))
	reg.Load(makeBookWithTags(2, "The Hobbit", "J.R.R. Tolkien", "fantasy"))

	// Before any query the results dir lists nothing.
	fstest.ChildCount(t, handle.results, 0)

	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	fstest.HasChild(t, handle.results, "Foundation")
	fstest.ChildCount(t, handle.results, 1)
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want %q", got, "tag:sci-fi")
	}

	// A matching book ingested after the query appears live.
	reg.Load(makeBookWithTags(3, "Dune", "Frank Herbert", "sci-fi"))
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
			tag := "sci-fi"
			if i%2 == 1 {
				tag = "fantasy"
			}
			reg.Load(makeBookWithTags(int64(i+1), fmt.Sprintf("Book %d", i+1), "Author", tag))
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

// The buffered-write contract: the query is accumulated by Write and only run at
// clunk, so a client that writes in several chunks gets one search rather than a
// partial one per write.
func TestSearchCtlExecutesQueryOnClunk(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	reg.Load(makeBookWithTags(1, "Foundation", "Isaac Asimov", "sci-fi"))
	reg.Load(makeBookWithTags(2, "The Hobbit", "J.R.R. Tolkien", "fantasy"))

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

	reg.Load(makeBookWithTags(1, "Foundation", "Isaac Asimov", "sci-fi"))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")

	fstest.Fid(t, ctl, 3).Write(0, "foo:bar")
	if err := ctl.Close(3); err == nil {
		t.Error("Close on an unparseable query returned nil, want the parse error surfaced to the client")
	}

	// The rejected query must leave the previous results standing.
	fstest.HasChild(t, handle.results, "Foundation")
	if got := handle.currentQueryText(); got != "tag:sci-fi" {
		t.Errorf("currentQueryText = %q, want the last accepted query %q", got, "tag:sci-fi")
	}
}

// The client-driven release path. Dropping the directory is only part of it:
// the results dir must also come off the registry, or a handle no client can
// reach goes on receiving every book event for the life of the process.
func TestSearchCtlCloseTearsDownHandle(t *testing.T) {
	reg, sd := newTestSearchDir(t, 0, 0)
	handle := sd.allocateHandle()
	ctl := ctlOf(t, handle)

	reg.Load(makeBookWithTags(1, "Foundation", "Isaac Asimov", "sci-fi"))
	handle.executeSearch(library.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	fstest.ChildCount(t, handle.results, 1)

	closer := fstest.Fid(t, ctl, 5)
	closer.Write(0, "close\n")
	closer.Close()

	if hasHandleDir(sd, handle.id) {
		t.Errorf(`handle %d still under search/ after writing "close", children: %v`, handle.id, fstest.ChildNames(sd))
	}

	reg.Load(makeBookWithTags(2, "Dune", "Frank Herbert", "sci-fi"))
	fstest.NoChild(t, handle.results, "Dune")

	// A client that clunks a second ctl fid carrying "close" must not take a
	// second teardown pass over an id that is already gone.
	again := fstest.Fid(t, ctl, 6)
	again.Write(0, "close")
	again.Close()
}
