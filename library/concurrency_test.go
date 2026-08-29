package library

import (
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
)

// TestEditWriteCoverConcurrentSameBook races an Edit that moves the book
// directory (title change) against a WriteCover on the same book. Both address
// the book by id and re-base on its current state under the per-book lock, so
// both must always succeed — whichever runs second resolves the post-move
// location — and both changes must be present in the final epub. Without the
// locked re-base the two rewriteEpub calls read the same pre-state and the
// last rename silently drops the other's change (lost update).
func TestEditWriteCoverConcurrentSameBook(t *testing.T) {
	lib := openTestLibrary(t)
	book := ingestTestEpub(t, lib, buildTestEpub(t, "Race Book"))
	id := book.ID()

	var newCover bytes.Buffer
	if err := jpeg.Encode(&newCover, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}

	titles := [2]string{"Race Book Alpha", "Race Book Beta"}
	root := lib.(*libraryImpl).store.Root()
	for i := range 10 {
		title := titles[i%2]

		var (
			wg       sync.WaitGroup
			edited   *Book
			editErr  error
			coverErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			edited, editErr = lib.Edit(id, Edits{Title: &title})
		}()
		go func() {
			defer wg.Done()
			coverData := newCover.Bytes()
			_, coverErr = lib.Edit(id, Edits{Cover: &coverData})
		}()
		wg.Wait()

		if editErr != nil {
			t.Fatalf("iteration %d: Edit: %v", i, editErr)
		}
		if coverErr != nil {
			t.Fatalf("iteration %d: WriteCover: %v", i, coverErr)
		}
		book = edited

		// The book must still be a valid epub at its new location, carrying
		// both changes.
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
			t.Fatalf("iteration %d: WriteCover reported success but cover bytes were lost", i)
		}

		// No stray temp files may accumulate in the book directory.
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
		books []*Book
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

	// Verify exactly one book exists in the library.
	got, err := lib.Search(Query{Authors: []string{"Alice"}})
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
