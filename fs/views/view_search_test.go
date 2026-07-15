package views

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func newTestSearchHandle(t *testing.T) (*registry.BookRegistry, *searchHandleDir) {
	t.Helper()
	f := newTestFS(t)
	reg := registry.NewBookRegistry(f, nil)
	sd := NewSearchDir(f, reg, 0, 0)
	return reg, sd.allocateHandle()
}

func TestSearchHandleResyncRebuildsMembership(t *testing.T) {
	reg, handle := newTestSearchHandle(t)

	b1 := makeBook(1, "Foundation", "Isaac Asimov")
	b1.Meta.Tags = []string{"sci-fi"}
	b2 := makeBook(2, "The Hobbit", "J.R.R. Tolkien")
	b2.Meta.Tags = []string{"fantasy"}
	reg.Add(b1)
	reg.Add(b2)

	// Before any query the results dir lists nothing.
	if n := len(dirChildNames(handle.results)); n != 0 {
		t.Fatalf("expected empty results before query, got %d entries", n)
	}

	handle.executeSearch(model.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
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
	reg.Add(b3)
	if _, ok := handle.results.Children()["Dune"]; !ok {
		t.Errorf("expected live-added Dune in results, got %v", dirChildNames(handle.results))
	}

	// Requerying rebuilds membership from scratch.
	handle.executeSearch(model.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
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
		for i := 0; i < 2*perTag; i++ {
			if i%2 == 0 {
				handle.executeSearch(model.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
			} else {
				handle.executeSearch(model.Query{Tags: []string{"fantasy"}}, "tag:fantasy")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2*perTag; i++ {
			b := makeBook(int64(i+1), fmt.Sprintf("Book %d", i+1), "Author")
			if i%2 == 0 {
				b.Meta.Tags = []string{"sci-fi"}
			} else {
				b.Meta.Tags = []string{"fantasy"}
			}
			reg.Add(b)
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2*perTag; i++ {
			_ = handle.currentQueryText()
			_ = handle.lastQuery()
		}
	}()
	wg.Wait()

	handle.executeSearch(model.Query{Tags: []string{"sci-fi"}}, "tag:sci-fi")
	if got := len(dirChildNames(handle.results)); got != perTag {
		t.Errorf("expected %d sci-fi results after quiescent requery, got %d", perTag, got)
	}
}
