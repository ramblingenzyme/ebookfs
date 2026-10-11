// Concurrent library edits against a BookDir, in process. The harness keeps
// one request in flight, so a torn read shows only by reading BookDir.Stat
// directly under -race.
package fs

import (
	"sync"
	"testing"

	go9pfs "github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestConcurrentLibraryEditsKeepBookDirSnapshotWhole(t *testing.T) {
	lib, _ := openLibrary(t)
	reg := registry.NewBookRegistry(util.NewTestFS(t), lib)
	lib.AddHook(reg)
	allBooks := views.NewAllBooksDir(reg)
	ingest("Race A", "Alice")(t, lib)
	book, err := lib.Search(library.Query{Titles: []string{"Race A"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(book) == 0 {
		t.Fatalf("no book found")
	}

	stableDir := fstest.ChildAs[go9pfs.Dir](t, allBooks, "Race A")
	titleFile := fstest.ChildAs[go9pfs.File](t, stableDir, "title")
	titles := [2]string{"Race A", "Race B"}

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		for i := range 100 {
			title := titles[i%len(titles)]
			if _, err := lib.Edit(book[0].ID(), library.Edits{Title: &title}); err != nil {
				t.Errorf("Edit: %v", err)
				return
			}
		}
	}()

	for range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				name := stableDir.Stat().Name
				if name != "Race A" && name != "Race B" {
					t.Errorf("BookDir exposed torn title %q", name)
					return
				}
				_ = titleFile.Stat()
			}
		}()
	}
	wg.Wait()
}
