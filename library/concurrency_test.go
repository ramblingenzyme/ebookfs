package library

import (
	"archive/zip"
	"bytes"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// buildTestEpub writes a minimal valid EPUB 3 with a cover entry and returns
// its bytes. The mimetype entry is STORED first per OCF.
func buildTestEpub(t *testing.T, title string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mt, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	mt.Write([]byte("application/epub+zip"))

	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">ebookfs-test-1</dc:identifier>
    <dc:title>` + title + `</dc:title>
    <dc:creator id="c1">Alice</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="cover" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
</package>`,
		"cover.jpg": "placeholder-cover-bytes",
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func openTestLibrary(t *testing.T) Library {
	t.Helper()
	dir := t.TempDir()
	lib, err := Open(config.LibraryConfig{
		Root:      filepath.Join(dir, "root"),
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}, false)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

func ingestTestEpub(t *testing.T, lib Library, data []byte) *model.Book {
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}

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
	id := book.Meta.ID

	var newCover bytes.Buffer
	if err := jpeg.Encode(&newCover, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}

	titles := [2]string{"Race Book Alpha", "Race Book Beta"}
	for i := range 10 {
		title := titles[i%2]

		var (
			wg       sync.WaitGroup
			edited   *model.Book
			editErr  error
			coverErr error
		)
		wg.Add(2)
		go func() {
			defer wg.Done()
			edited, editErr = lib.Edit(id, model.Edits{Title: &title})
		}()
		go func() {
			defer wg.Done()
			coverErr = lib.WriteCover(id, newCover.Bytes())
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
		parsed, err := epub.Parse(book.EpubPath)
		if err != nil {
			t.Fatalf("iteration %d: final epub does not parse: %v", i, err)
		}
		if parsed.Title != title {
			t.Fatalf("iteration %d: title = %q, want %q (Edit's change was lost)", i, parsed.Title, title)
		}
		got, err := epub.ExtractCover(book.EpubPath, book.CoverPath)
		if err != nil {
			t.Fatalf("iteration %d: ExtractCover: %v", i, err)
		}
		if !bytes.Equal(got, newCover.Bytes()) {
			t.Fatalf("iteration %d: WriteCover reported success but cover bytes were lost", i)
		}

		// No stray temp files may accumulate in the book directory.
		entries, err := os.ReadDir(filepath.Dir(book.EpubPath))
		if err != nil {
			t.Fatalf("iteration %d: %v", i, err)
		}
		for _, e := range entries {
			if name := e.Name(); name != book.EpubFilename && name != "meta.toml" {
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
		books []*model.Book
		errs  []error
	)

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
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
		}()
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
	got, err := lib.Query(model.Filter{Author: "Alice"})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 book by Alice, got %d", len(got))
	}
	if got[0].Title != "Concurrent Dupe" {
		t.Errorf("book title = %q, want %q", got[0].Title, "Concurrent Dupe")
	}
}
