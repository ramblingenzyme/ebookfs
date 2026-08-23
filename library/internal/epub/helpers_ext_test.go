// Helpers shared by both external test files: the corpus builders that turn a
// slice of <metadata> into a real EPUB on disk, and the readers that get the
// OPF back out again.
package epub_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const (
	opfPath = "OEBPS/content.opf"

	// The OCF names, redeclared here rather than reached for in the package
	// under test: these tests drive epub from the outside, and the spec fixes
	// both values (§4.3.3), so a test asserting them is asserting the spec
	// rather than whatever the implementation happens to call them.
	mimetypePath  = "mimetype"
	mimetypeValue = "application/epub+zip"
)

// writeBib applies edits to the package document of the epub at epubPath,
// rewrites the file in place, and returns the re-parsed Book. Production code
// drives that flow through library.Edit.
func writeBib(epubPath string, e model.Edits) (model.Bib, error) {
	return epub.Rewrite(epubPath, &model.Book{Location: model.Location{EpubPath: epubPath}}, e)
}

// writeCover replaces the cover image entry (coverPath, as resolved by Parse)
// with img, rewrites the file in place, and returns the re-parsed Book.
func writeCover(epubPath, coverPath string, img []byte) (model.Bib, error) {
	return epub.Rewrite(epubPath, &model.Book{Location: model.Location{EpubPath: epubPath}, Bib: model.Bib{CoverPath: coverPath}}, model.Edits{Cover: &img})
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

// opf3With returns opf3 with extra metadata spliced in before </metadata>, so a
// fixture that needs one more <meta> does not restate the whole package.
func opf3With(extra string) string {
	const close = "  </metadata>"
	before, after, ok := strings.Cut(opf3, close)
	if !ok {
		panic("opf3 has no </metadata>")
	}
	return before + extra + "\n" + close + after
}

var (
	chapterBytes = []byte("<html><body><p>chapter one</p></body></html>")
	coverBytes   = []byte("ORIGINAL-COVER-BYTES")
)

func baseEntries(opf string, extra ...entry) []entry {
	es := []entry{
		{name: "mimetype", data: []byte(mimetypeValue), store: true},
		{name: "META-INF/container.xml", data: []byte(containerXML)},
		{name: "OEBPS/content.opf", data: []byte(opf)},
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	}
	return append(es, extra...)
}

func metadata(t *testing.T, path string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, opfPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		t.Fatal("result has no <metadata>")
	}
	return md
}

// epub3 and epub2 wrap a <metadata> body in the smallest conforming package of
// each version, so a fixture shows only the metadata its test is about. The
// internal tests have the same helper as opf3With; it is not reachable from
// this package. Tests needing a different manifest keep a full literal.
func epub3(meta string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`
}

func epub2(meta string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`
}

// book builds the Book that Rewrite validates against, the way library.Edit
// does: from the file's current state. An index edit is refused unless the book
// already has a series, so an empty Bib is not a usable stand-in.
func book(t *testing.T, path string) *model.Book {
	t.Helper()
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Book{Location: model.Location{EpubPath: path}, Bib: *bib}
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

// buildEpub writes the standard five-entry archive around a package document.
// The layout lives in baseEntries so there is one definition of what a fixture
// epub looks like, and writeEpub is the only thing in these tests that writes a
// zip; tests needing a different archive call those two directly.
func buildEpub(t *testing.T, opf string) string {
	t.Helper()
	return writeEpub(t, baseEntries(opf))
}

// readEntry returns the entry's bytes, failing the test when it is absent.
// readEntryFromFile is the same lookup for tests that need to assert on absence.
func readEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	b, ok := readEntryFromFile(t, path, name)
	if !ok {
		t.Fatalf("entry %q not found", name)
	}
	return b
}
