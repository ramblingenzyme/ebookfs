// Concurrency across Edit and ingest. Pins two races: a cover edit racing a
// title edit on one book, and two ingests of one new book, where exactly one
// wins and the other gets ErrDuplicate.

package library_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// A title edit, which moves the book directory, races a cover edit on the same
// book. Both re-read the book under the per-book lock, so both succeed and both
// changes land. Without the lock, the two epub.Rewrite calls read the same
// starting state and the second rename silently drops the first's change.
func TestConcurrentTitleAndCoverEditsBothLand(t *testing.T) {
	cfg := testConfig(t)
	lib := openLib(t, cfg)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Race Book"))
	id := book.ID()

	var newCover bytes.Buffer
	if err := jpeg.Encode(&newCover, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}

	titles := [2]string{"Race Book Alpha", "Race Book Beta"}
	root := cfg.Root
	for i := range 10 {
		title := titles[i%2]

		var (
			wg       sync.WaitGroup
			edited   *library.Book
			editErr  error
			coverErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			edited, editErr = lib.Edit(id, library.Edits{Title: &title})
		}()
		go func() {
			defer wg.Done()
			coverData := newCover.Bytes()
			_, coverErr = lib.Edit(id, library.Edits{Cover: &coverData})
		}()
		wg.Wait()

		if editErr != nil {
			t.Fatalf("iteration %d: Edit: %v", i, editErr)
		}
		if coverErr != nil {
			t.Fatalf("iteration %d: cover edit: %v", i, coverErr)
		}
		book = edited

		parsed, err := epub.Parse(filepath.Join(root, book.EpubPath()))
		if err != nil {
			t.Fatalf("iteration %d: final epub does not parse: %v", i, err)
		}
		if parsed.Title != title {
			t.Fatalf("iteration %d: title = %q, want %q (Edit's change was lost)", i, parsed.Title, title)
		}
		reader, err := epub.OpenReader(filepath.Join(root, book.EpubPath()), book.CoverPath())
		if err != nil {
			t.Fatalf("iteration %d: OpenReader: %v", i, err)
		}
		defer reader.Close()
		got, err := reader.Cover()
		if err != nil {
			t.Fatalf("iteration %d: Cover: %v", i, err)
		}
		if !bytes.Equal(got, newCover.Bytes()) {
			t.Fatalf("iteration %d: cover edit reported success but the cover bytes were lost", i)
		}

		entries, err := os.ReadDir(filepath.Join(root, filepath.Dir(book.EpubPath())))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		for _, e := range entries {
			if name := e.Name(); name != filepath.Base(book.EpubPath()) && name != "meta.toml" {
				t.Fatalf("iteration %d: unexpected file in book dir: %q", i, name)
			}
		}
	}
}

func TestConcurrentDuplicateIngestRejected(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Concurrent Dupe")

	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		count int
		books []*library.Book
		errs  []error
	)

	for range 3 {
		wg.Go(func() {
			h, err := lib.CreateIngest()
			if err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			if _, err := h.WriteAt(data, 0); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				return
			}
			b, err := h.Ingest()
			mu.Lock()
			if err != nil {
				errs = append(errs, err)
			} else {
				books = append(books, b)
			}
			count++
			mu.Unlock()
		})
	}
	wg.Wait()

	if count != 3 {
		t.Fatalf("expected 3 ingests to complete (some with errors), got %d", count)
	}
	if len(books) != 1 {
		t.Fatalf("expected exactly 1 successful ingest, got %d", len(books))
	}
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 errors for duplicate ingests, got %d", len(errs))
	}

	got, err := lib.Search(library.Query{Authors: []string{"Alice"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 book by Alice, got %d", len(got))
	}
	if got[0].Title() != "Concurrent Dupe" {
		t.Errorf("book title = %q, want %q", got[0].Title(), "Concurrent Dupe")
	}
}
