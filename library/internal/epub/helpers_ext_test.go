// Helpers shared by both external test files: the corpus builders that turn a
// slice of <metadata> into a real EPUB on disk, and the readers that get the
// OPF back out again.
package epub_test

import (
	"archive/zip"
	"embed"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/edits"
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
func writeBib(epubPath string, e edits.Edits) (bookmodel.Bib, error) {
	return epub.Rewrite(epubPath, &bookmodel.Book{Location: bookmodel.Location{EpubPath: epubPath}}, e)
}

// writeCover replaces the cover image entry (coverPath, as resolved by Parse)
// with img, rewrites the file in place, and returns the re-parsed Book.
func writeCover(epubPath, coverPath string, img []byte) (bookmodel.Bib, error) {
	return epub.Rewrite(epubPath, &bookmodel.Book{Location: bookmodel.Location{EpubPath: epubPath}, Bib: bookmodel.Bib{CoverPath: coverPath}}, edits.Edits{Cover: &img})
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

// A package must carry an identifier, a title and a language (EPUB 3.3 §3.4.3),
// and only four tests in this suite are about any of them. These values are
// deliberately uninteresting: seeing one in a failure message means the test
// was not about it.
const (
	fillIdentifier = `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>`
	fillTitle      = `    <dc:title>The Title</dc:title>`
	fillCreator    = `    <dc:creator id="c1">Ann Rand</dc:creator>`
	fillLanguage   = `    <dc:language>en</dc:language>`
)

// required returns the elements above that meta does not already declare, so a
// fixture shows only the metadata its test is about. A test that cares about one
// of them writes it, and its own version wins. Detection is by element name,
// which is enough for hand-written fixtures.
//
// A package deliberately missing one of these needs a full literal instead. No
// test needs that today: the parse tests reject a bad archive through
// withMimetype and withContainer, a layer below this one.
func required(meta string) string {
	var out []string
	for _, f := range []struct{ tag, line string }{
		{"<dc:identifier", fillIdentifier},
		{"<dc:title", fillTitle},
		{"<dc:creator", fillCreator},
		{"<dc:language", fillLanguage},
	} {
		if !strings.Contains(meta, f.tag) {
			out = append(out, f.line)
		}
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// epub3 and epub2 wrap a <metadata> body in the smallest conforming package of
// each version. The internal tests have the same helper as opf3With; it is not
// reachable from this package. Tests needing a different manifest keep a full
// literal.
func epub3(meta string, manifest ...string) string {
	meta = required(meta) + meta
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    ` + manifestOr(manifest, `<item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`) + `
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`
}

// manifestOr lets a test state its own manifest, which the cover-resolution
// tests need: the default carries a cover item and those tests are about what
// happens without one.
func manifestOr(given []string, fallback string) string {
	if len(given) == 1 {
		return given[0]
	}
	return fallback
}

func epub2(meta string, manifest ...string) string {
	meta = required(meta) + meta
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    ` + manifestOr(manifest, `<item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`) + `
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`
}

// book builds the Book that Rewrite validates against, the way library.Edit
// does: from the file's current state. An index edit is refused unless the book
// already has a series, so an empty Bib is not a usable stand-in.
func book(t *testing.T, path string) *bookmodel.Book {
	t.Helper()
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return &bookmodel.Book{Location: bookmodel.Location{EpubPath: path}, Bib: *bib}
}

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

// legacyMeta and property read the two encodings of the same idea. An EPUB 2
// <meta name="x" content="v"/> carries its value in an attribute; an EPUB 3
// <meta property="x">v</meta> carries it as text. Which one a file uses is what
// half these tests turn on, so the difference is named here instead of respelled
// at every assertion.
//
// Both fail when nothing matches, and both return a value rather than comparing
// it: the caller's own message names the spec consequence, and "%v" on an
// *etree.Element prints etree's struct rather than the value in question.
func legacyMeta(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return attrOf(t, root, path, "content")
}

func property(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return elemAt(t, root, path).Text()
}

// attrOf and elemAt are the same lookup for the elements that are not <meta>:
// a dc element's text, or an attribute the spec puts somewhere else.
func attrOf(t *testing.T, root *etree.Element, path, attr string) string {
	t.Helper()
	return elemAt(t, root, path).SelectAttrValue(attr, "")
}

func textOf(t *testing.T, root *etree.Element, path string) string {
	t.Helper()
	return elemAt(t, root, path).Text()
}

func elemAt(t *testing.T, root *etree.Element, path string) *etree.Element {
	t.Helper()
	el := root.FindElement(path)
	if el == nil {
		t.Fatalf("nothing matches %s; <metadata> holds %s", path, childTags(root))
	}
	return el
}

// childTags names what is there, since a failed lookup is most often a element
// written under the wrong parent rather than one that vanished.
func childTags(root *etree.Element) string {
	var tags []string
	for _, c := range root.ChildElements() {
		tags = append(tags, c.Tag)
	}
	return strings.Join(tags, ", ")
}

// The complete documents these tests share, kept as files so they read as XML
// and diff as XML. Each one is used whole; a fixture that varies a single
// element stays inline beside the test that varies it.
//
// The whole directory is embedded rather than each file, so adding a document
// is adding a file.

//go:embed testdata
var fixtures embed.FS

// fixture drops the trailing newline a text file ends in. These go into epub
// entries verbatim, and one test compares a rewritten cover page against the
// document it started from, so the extra byte is not inert. It panics rather
// than taking a testing.T because the vars below are package-level: a missing
// fixture is a broken build, not a failing test.
func fixture(name string) string {
	b, err := fixtures.ReadFile("testdata/" + name)
	if err != nil {
		panic(err)
	}
	return strings.TrimRight(string(b), "\n")
}

var (
	opf3        = fixture("package-epub3.opf")
	opf2        = fixture("package-epub2.opf")
	richOPF3    = fixture("rich-epub3.opf")
	richOPF2    = fixture("rich-epub2.opf")
	ncxOPF      = fixture("ncx-package.opf")
	opfWrappers = fixture("legacy-wrappers.opf")

	// The shape calibre and Sigil both produce.
	svgCoverPage = fixture("svg-cover-page.xhtml")

	// Two package rootfiles where the first does not exist in the zip, the
	// shape seen in some Kobo epubs. It has no builder because two rootfiles is
	// the shape being tested, not a value inside one.
	multiRootContainer = fixture("container-multi-root.xml")
)

var containerXML = containerFor(opfPath, packageMediaType)

// metas joins metadata parts into a <metadata> body, indenting each line, so a
// fixture composes from named pieces instead of splicing calls into a backtick
// string. A raw XML line is a valid part, which is what lets a fixture name the
// pieces it is about and write out the one it is not.
func metas(parts ...string) string {
	var out []string
	for _, part := range parts {
		for _, line := range strings.Split(part, "\n") {
			if line == "" {
				out = append(out, "")
				continue
			}
			out = append(out, "    "+line)
		}
	}
	return strings.Join(out, "\n")
}

// collection is the EPUB 3 series encoding: the collection, the collection-type
// refinement saying what kind it is, and a group-position when the book has one
// (§5.5.3.3, D.3.3). An empty position is a book in a series with no stated place
// in it. parent nests this collection inside another, which D.3.3 allows and
// which only a series inside a set uses.
func collection(id, name, kind, position string, parent ...string) string {
	refines := ""
	if len(parent) == 1 {
		refines = ` refines="#` + parent[0] + `"`
	}
	out := `<meta property="belongs-to-collection" id="` + id + `"` + refines + `>` + name + `</meta>
<meta refines="#` + id + `" property="collection-type">` + kind + `</meta>`
	if position != "" {
		out += "\n" + `<meta refines="#` + id + `" property="group-position">` + position + `</meta>`
	}
	return out
}

// calibreSeries is the EPUB 2 encoding of the same thing, the pair of metas
// calibre writes. An empty index omits calibre:series_index, which is what a
// series carrying no position looks like on the way out.
func calibreSeries(name, index string) string {
	out := `<meta name="calibre:series" content="` + name + `"/>`
	if index != "" {
		out += "\n" + `<meta name="calibre:series_index" content="` + index + `"/>`
	}
	return out
}

// packageMediaType is how a reader decides a rootfile is the package document
// (OCF 3.3 §4.2.1). Declared here rather than reached for in epub, since these
// tests drive it from the outside and the spec fixes the value.
const packageMediaType = "application/oebps-package+xml"

// wrapped is an attribute value split across lines the way an editor breaks a
// long one. The newlines are the subject: XML 1.0 §3.3.3 turns each into a
// space, so the value arrives padded.
func wrapped(value string) string { return "\n        " + value + "\n      " }

const (
	aes256     = "http://www.w3.org/2001/04/xmlenc#aes256-cbc"
	fontObfusc = "http://www.idpf.org/2008/embedding"
)

// encryptionXML is META-INF/encryption.xml naming one encrypted resource. The
// two arguments are what the tests vary: the algorithm says whether the file is
// really encrypted or only font-obfuscated (OCF 3.3 §4.5), and the URI says
// which entry it covers.
func encryptionXML(algorithm, uri string) string {
	return `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="` + algorithm + `"/>
    <enc:CipherData><enc:CipherReference URI="` + uri + `"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
}

// containerFor is META-INF/container.xml naming one rootfile. Both arguments are
// what the tests vary: the path, which may be percent-encoded or name an entry
// that is not in the archive, and the media type, which decides whether the
// rootfile is recognised at all.
func containerFor(fullPath, mediaType string) string {
	return `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="` + fullPath + `" media-type="` + mediaType + `"/>
  </rootfiles>
</container>`
}
