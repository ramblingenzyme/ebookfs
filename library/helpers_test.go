package library

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// buildTestEpub writes a minimal valid EPUB 3 with a cover entry and returns
// its bytes. The mimetype entry is STORED first per OCF. If authors are
// omitted, defaults to ["Alice"].
func buildTestEpub(t *testing.T, title string, authors ...string) []byte {
	t.Helper()
	if len(authors) == 0 {
		authors = []string{"Alice"}
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	mt, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	mt.Write([]byte("application/epub+zip"))

	var creatorEls string
	for i, a := range authors {
		creatorEls += fmt.Sprintf("    <dc:creator id=\"c%d\">%s</dc:creator>\n", i+1, a)
	}

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
` + creatorEls + `    <dc:language>en</dc:language>
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

// testConfig returns a library config rooted in a fresh temp dir. Tests that
// reopen a library across restarts need the config itself, not just the opened
// library, so the layout is stated here once.
func testConfig(t *testing.T) config.LibraryConfig {
	t.Helper()
	dir := t.TempDir()
	return config.LibraryConfig{
		Root:      filepath.Join(dir, "root"),
		InboxTemp: filepath.Join(dir, "inbox-tmp"),
		IndexPath: filepath.Join(dir, "index.db"),
	}
}

func openTestLibrary(t *testing.T) Library {
	t.Helper()
	return openLib(t, testConfig(t), false)
}

// openLib opens a library at cfg and registers its close, so an assertion that
// fails mid-test cannot leave the index open. Tests that reopen across a
// simulated restart still Close explicitly for sequencing; the second close is
// a no-op.
func openLib(t *testing.T, cfg config.LibraryConfig, forceReindex bool) Library {
	t.Helper()
	lib, err := Open(cfg, forceReindex)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

// ptr matches the helper the model and epub test packages already use.
func ptr[T any](v T) *T { return &v }

// drifted reports storeDrifted's verdict, discarding the store scan it returns
// for the reindex path to reuse.
func drifted(t *testing.T, lib Library) bool {
	t.Helper()
	_, d := lib.(*libraryImpl).storeDrifted()
	return d
}

// metaPathOf returns the path of book's meta.toml sidecar. The store keeps the
// filename private, so tests that reach around the library restate it here once.
func metaPathOf(book *model.Book) string {
	return filepath.Join(filepath.Dir(book.EpubPath), "meta.toml")
}

// breakEpub replaces book's epub with a symlink to nothing. store.Walk still
// reports the directory — findEpub only reads the directory entry — while
// os.Stat follows the link and fails, which is the one way to reach the
// rebuild's "could not observe this book at all" path from a test.
func breakEpub(t *testing.T, book *model.Book) {
	t.Helper()
	if err := os.Remove(book.EpubPath); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(book.EpubPath), "nowhere.epub")
	if err := os.Symlink(dangling, book.EpubPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
}

// dropIndex deletes the index database, so the next Open rebuilds it from the
// store alone. That is the state the id-reservation logic exists for: the id
// sequence lives in the index, so a rebuild that starts without one has nothing
// but the store to learn which ids are already spoken for. Rebuild leaves the
// sequence table alone otherwise, which is why merely reindexing does not
// exercise this.
func dropIndex(t *testing.T, cfg config.LibraryConfig) {
	t.Helper()
	// The WAL and shared-memory sidecars would otherwise resurrect the sequence.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(cfg.IndexPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove index%s: %v", suffix, err)
		}
	}
}

// assertSettlesClean pins that a library at cfg stops drifting once it has been
// rebuilt: the first Open reindexes and records what it found, and every Open
// after that must see a clean index. A book the rebuild cannot read is the case
// that breaks this — if the rebuild forgets it, drift detection sees a directory
// on disk it cannot account for and reindexes the whole library on every startup.
//
// Two restarts is the whole proof: one to record, one to confirm the record is
// believed. Both are closed explicitly rather than through openLib, since the
// point is what a later process sees after this one has let go.
func assertSettlesClean(t *testing.T, cfg config.LibraryConfig) {
	t.Helper()
	for i := 1; i <= 2; i++ {
		lib, err := Open(cfg, false)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		d := drifted(t, lib)
		if err := lib.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
		if d {
			t.Fatalf("storeDrifted() = true on restart %d — the rebuild did not record what it found, "+
				"so one unreadable book forces a full reindex on every startup", i)
		}
	}
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
