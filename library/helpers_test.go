package library

import (
	"archive/zip"
	"bytes"
	"fmt"
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
