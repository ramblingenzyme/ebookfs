package epub

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Shared test fixtures and the epub builder used by both parse_test.go and
// write_test.go.

// writeBib applies edits to the package document of the epub at epubPath,
// rewrites the file in place, and returns the re-parsed Book. A test-only
// convenience over Prepare/Commit; production code drives that flow through
// library.Edit.
func writeBib(epubPath string, e model.Edits) (*Book, error) {
	c, err := Prepare(&model.Book{Location: model.Location{EpubPath: epubPath}}, e)
	if err != nil {
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		return nil, err
	}
	return c.Book(), nil
}

// writeCover replaces the cover image entry (coverPath, as resolved by Parse)
// with img, rewrites the file in place, and returns the re-parsed Book.
func writeCover(epubPath, coverPath string, img []byte) (*Book, error) {
	c, err := Prepare(&model.Book{Location: model.Location{EpubPath: epubPath}, Bib: model.Bib{CoverPath: coverPath}}, model.Edits{Cover: &img})
	if err != nil {
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		return nil, err
	}
	return c.Book(), nil
}

type entry struct {
	name  string
	data  []byte
	store bool // stored (uncompressed) rather than deflated
}

func writeEpub(t *testing.T, entries []entry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	for _, e := range entries {
		method := zip.Deflate
		if e.store {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

const opf3 = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <dc:title id="t1">Original Title</dc:title>
    <meta refines="#t1" property="file-as">Title, Original</meta>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#creator1" property="file-as">Doe, Jane</meta>
    <dc:language>en</dc:language>
    <dc:date>2020-01-02</dc:date>
    <dc:description>Original description.</dc:description>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

const opf2 = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid" opf:scheme="uuid">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator opf:role="aut" opf:file-as="Doe, Jane">Jane Doe</dc:creator>
    <dc:language>en</dc:language>
    <dc:date>2020-01-02</dc:date>
    <dc:description>Original description.</dc:description>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`

var (
	chapterBytes = []byte("<html><body><p>chapter one</p></body></html>")
	coverBytes   = []byte("ORIGINAL-COVER-BYTES")
)

func baseEntries(opf string, extra ...entry) []entry {
	es := []entry{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
		{name: "META-INF/container.xml", data: []byte(containerXML)},
		{name: "OEBPS/content.opf", data: []byte(opf)},
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	}
	return append(es, extra...)
}
