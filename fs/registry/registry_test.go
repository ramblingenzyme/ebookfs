package registry

import (
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// fakeView is a minimal BookView; the registry only needs a registered view to
// exercise the remove/add bracketing of a commit. It records nothing — the
// concurrency assertions read the BookDir snapshot directly.
type fakeView struct{}

func (fakeView) Add(*book.BookDir)    {}
func (fakeView) Remove(*book.BookDir) {}

func TestEditUnknownID(t *testing.T) {
	reg := NewBookRegistry(testutil.NewTestFS(t), libfake.Lib{})

	status := "read"
	if err := reg.edit(999, model.Edits{Status: &status}); err == nil {
		t.Fatal("expected error editing unknown book")
	}
}

// TestEditConcurrentSnapshotSwap exercises the snapshot swap under the
// concurrency go9p produces: handler goroutines read a BookDir's Stat/Children
// with no registry lock while edit commits swap the snapshot under r.mu. Run
// with -race to verify; without it the test still asserts a reader never
// observes a torn snapshot (a name that is neither the old nor the new title).
func TestEditConcurrentSnapshotSwap(t *testing.T) {
	// current mimics the library's authoritative state; EditFn runs under the
	// registry mutex, so reading and replacing it is serialized.
	current := testutil.MakeBook(1, "Title A", "Alice")
	lib := libfake.Lib{
		EditFn: func(id int64, e model.Edits) (*model.Book, error) {
			updated := *current
			if e.Title != nil {
				updated.Title = *e.Title
			}
			current = &updated
			return &updated, nil
		},
	}
	reg := NewBookRegistry(testutil.NewTestFS(t), lib)
	reg.AddView(fakeView{})
	reg.Add(current)

	// White-box: reach the stable BookDir the registry created.
	bd := reg.books[1]
	titleFF := bd.Children()["title"]

	done := make(chan struct{})
	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(done)
		titles := [2]string{"Title B", "Title A"}
		for i := range 200 {
			title := titles[i%2]
			if err := reg.edit(1, model.Edits{Title: &title}); err != nil {
				t.Errorf("edit: %v", err)
				return
			}
		}
	}()

	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
				}
				if name := bd.Stat().Name; name != "Title A" && name != "Title B" {
					t.Errorf("torn BookDir name: %q", name)
					return
				}
				titleFF.Stat() // field get closure reads the snapshot
			}
		}()
	}
	wg.Wait()
}
