// Registry hook scenarios pin the library-to-view path: public library
// operations publish events, and the registry moves stable BookDirs in views.
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

func TestLibraryMutationsUpdateSubscribedViews(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	lib.AddHook(reg)

	allBooks := views.NewAllBooksDir(reg)
	byAuthor := views.NewByAuthorDir(reg)
	byID := views.NewByIDDir(reg)
	byStatus := views.NewByStatusDir(reg)

	b := ingestRegistryScenarioBook(t, lib, "Before", "Alice")
	fstest.HasChild(t, allBooks, "Before")
	fstest.HasChild(t, fstest.ChildAs[go9pfs.Dir](t, byAuthor, "Alice"), "Before")

	title := "After"
	if _, err := lib.Edit(b.ID(), library.Edits{Title: &title}); err != nil {
		t.Fatalf("Edit title: %v", err)
	}
	fstest.NoChild(t, allBooks, "Before")
	fstest.HasChild(t, allBooks, "After")
	fstest.HasChild(t, byID, "1. After")

	status := "reading"
	if _, err := lib.Edit(b.ID(), library.Edits{Status: &status}); err != nil {
		t.Fatalf("Edit status: %v", err)
	}
	fstest.HasChild(t, fstest.ChildAs[go9pfs.Dir](t, byStatus, "reading"), "After")

	if err := lib.Delete(b.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	fstest.NoChild(t, allBooks, "After")
	fstest.NoChild(t, byID, "1. After")
}

func TestRegistryLoadSeedsBooksAlreadyInLibrary(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	ingestRegistryScenarioBook(t, lib, "Existing", "Alice")

	reg := registry.NewBookRegistry(util.NewTestFS(t), lib)
	allBooks := views.NewAllBooksDir(reg)
	books, err := lib.Search(library.Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	reg.Load(books...)

	fstest.HasChild(t, allBooks, "Existing")
	if got := fstest.ChildAs[go9pfs.Dir](t, allBooks, "Existing").Stat().Name; got != "Existing" {
		t.Errorf("loaded view entry name = %q, want Existing", got)
	}
}

func TestConcurrentLibraryEditsKeepBookDirSnapshotWhole(t *testing.T) {
	lib := openRegistryScenarioLibrary(t)
	reg := registry.NewBookRegistry(util.NewTestFS(t), lib)
	lib.AddHook(reg)
	allBooks := views.NewAllBooksDir(reg)
	book := ingestRegistryScenarioBook(t, lib, "Race A", "Alice")

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
			if _, err := lib.Edit(book.ID(), library.Edits{Title: &title}); err != nil {
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

func openRegistryScenarioLibrary(t *testing.T) *library.Library {
	t.Helper()
	lib, err := library.Open(library.Config(util.TestConfig(t)))
	if err != nil {
		t.Fatalf("Open library: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })
	return lib
}

func ingestRegistryScenarioBook(t *testing.T, lib *library.Library, title, author string) *library.Book {
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(util.BuildTestEpub(t, title, author), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}
