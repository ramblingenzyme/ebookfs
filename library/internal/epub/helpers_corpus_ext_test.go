// The corpus these tests drive: the archive on disk, the package document
// inside it, and the two calls that put an edit through. A document assembled
// from parts is built here; a complete one kept as a file is in
// helpers_fixtures_ext_test.go.

package epub_test

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"

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

// packageDoc is an OPF package document. It stays a string, so a raw literal is
// still one and a fixture can drop out to XML wherever it needs to.
type packageDoc string

// with splices metadata in before </metadata>, so a test varying one <meta> on a
// complete fixture does not restate the whole document.
func (d packageDoc) with(parts ...string) packageDoc {
	const close = "  </metadata>"
	before, after, ok := strings.Cut(string(d), close)
	if !ok {
		panic("package document has no </metadata>")
	}
	return packageDoc(before + strings.Join(parts, "\n") + "\n" + close + after)
}

// pkg is a package document described by its parts. Every field but meta has a
// default, so a test writes only what it is about, and the document is built
// once rather than built and then patched.
type pkg struct {
	meta     string // the <metadata> body; required elements are filled in
	manifest string // default: the cover image and one chapter
	spine    string // an itemref placed before ch1, e.g. a cover page
	guide    bool   // add the OPF 2.0 <guide> cover reference
	attrs    string // extra attributes on <package>, e.g. a prefix binding
}

// epub3 and epub2 render p as the smallest conforming package of each version.
// The manifest differs because EPUB 3 marks the cover with a property and EPUB 2
// has no such thing; the spine differs because EPUB 2 points at the NCX.
func (p pkg) epub3() packageDoc {
	return p.build("3.0", "",
		`<item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`, "")
}

func (p pkg) epub2() packageDoc {
	return p.build("2.0", ` xmlns:opf="http://www.idpf.org/2007/opf"`,
		`<item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`, ` toc="ncx"`)
}

func (p pkg) build(version, opfNS, defaultManifest, spineAttr string) packageDoc {
	manifest := p.manifest
	if manifest == "" {
		manifest = defaultManifest
	}
	attrs := ""
	if p.attrs != "" {
		attrs = " " + p.attrs
	}
	spine := ""
	if p.spine != "" {
		spine = `<itemref idref="` + p.spine + `"/>`
	}
	guide := ""
	if p.guide {
		guide = "\n" + `  <guide><reference type="cover" href="cover.xhtml" title="Cover"/></guide>`
	}
	return packageDoc(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf"` + opfNS + ` version="` + version + `" unique-identifier="pub-id"` + attrs + `>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + strings.TrimRight(required(p.meta)+p.meta, "\n") + `
  </metadata>
  <manifest>
    ` + manifest + `
  </manifest>
  <spine` + spineAttr + `>` + spine + `<itemref idref="ch1"/></spine>` + guide + `
</package>`)
}

var (
	chapterBytes = []byte("<html><body><p>chapter one</p></body></html>")
	coverBytes   = []byte("ORIGINAL-COVER-BYTES")
)

func baseEntries(opf packageDoc, extra ...entry) []entry {
	es := []entry{
		{name: "mimetype", data: []byte(mimetypeValue), store: true},
		{name: "META-INF/container.xml", data: []byte(containerXML)},
		{name: "OEBPS/content.opf", data: []byte(string(opf))},
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	}
	return append(es, extra...)
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

// epub3 and epub2 are the common case: a package that varies only in its
// metadata. A test varying anything else writes a pkg literal.
func epub3(meta string) packageDoc { return pkg{meta: meta}.epub3() }
func epub2(meta string) packageDoc { return pkg{meta: meta}.epub2() }

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
func buildEpub(t *testing.T, opf packageDoc) string {
	t.Helper()
	return writeEpub(t, baseEntries(opf))
}
