// Package epubtest builds the epub corpus both epub test suites drive: the
// archive on disk, the package document inside it, and the assertions that read
// one back. The public epub package tests the format; library/internal/epub
// tests the translation to the library's model. Both need the same fixtures, and
// neither can reach the other's test files.
//
// A document assembled from named parts is built here. A complete one kept as a
// file is in fixtures.go, so it reads as XML and diffs as XML.
//
// Nothing here imports either epub package. These helpers describe what an epub
// looks like, which is the spec's business rather than any implementation's, and
// a fixture that reached into the code under test would assert that code against
// itself.
package epubtest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	// OPFPath is where these fixtures put the package document.
	OPFPath = "OEBPS/content.opf"

	// The OCF names, redeclared here rather than reached for in the package
	// under test: these tests drive epub from the outside, and the spec fixes
	// both values (§4.3.3), so a test asserting them is asserting the spec
	// rather than whatever the implementation happens to call them.
	MimetypePath  = "mimetype"
	MimetypeValue = "application/epub+zip"
)

// Entry is one zip entry of a fixture archive.
type Entry struct {
	Name  string
	Data  []byte
	Store bool // stored (uncompressed) rather than deflated
}

// WriteEpub writes entries to a zip in a temp dir and returns its path. It is
// the only thing in these suites that writes a zip.
func WriteEpub(t *testing.T, entries []Entry) string {
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
		if e.Store {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.Name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(e.Data); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

// ChapterOnlyManifest is a manifest with no cover item, for the tests about a
// package that declares its own cover — or declares none at all.
const ChapterOnlyManifest = `<item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`

// PackageDoc is an OPF package document. It stays a string, so a raw literal is
// still one and a fixture can drop out to XML wherever it needs to.
type PackageDoc string

// With splices metadata in before </metadata>, so a test varying one <meta> on a
// complete fixture does not restate the whole document.
func (d PackageDoc) With(parts ...string) PackageDoc {
	const close = "  </metadata>"
	before, after, ok := strings.Cut(string(d), close)
	if !ok {
		panic("package document has no </metadata>")
	}
	return PackageDoc(before + strings.Join(parts, "\n") + "\n" + close + after)
}

// Pkg is a package document described by its parts. Every field but Meta has a
// default, so a test writes only what it is about, and the document is built
// once rather than built and then patched.
type Pkg struct {
	Meta     string // the <metadata> body; required elements are filled in
	Manifest string // default: the cover image and one chapter
	Spine    string // an itemref placed before ch1, e.g. a cover page
	Guide    bool   // add the OPF 2.0 <guide> cover reference
	Attrs    string // extra attributes on <package>, e.g. a prefix binding
}

// EPUB3 and EPUB2 render p as the smallest conforming package of each version.
// The manifest differs because EPUB 3 marks the cover with a property and EPUB 2
// has no such thing; the spine differs because EPUB 2 points at the NCX.
func (p Pkg) EPUB3() PackageDoc {
	return p.build("3.0", "",
		`<item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`, "")
}

func (p Pkg) EPUB2() PackageDoc {
	return p.build("2.0", ` xmlns:opf="http://www.idpf.org/2007/opf"`,
		`<item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`, ` toc="ncx"`)
}

func (p Pkg) build(version, opfNS, defaultManifest, spineAttr string) PackageDoc {
	manifest := p.Manifest
	if manifest == "" {
		manifest = defaultManifest
	}
	attrs := ""
	if p.Attrs != "" {
		attrs = " " + p.Attrs
	}
	spine := ""
	if p.Spine != "" {
		spine = `<itemref idref="` + p.Spine + `"/>`
	}
	guide := ""
	if p.Guide {
		guide = "\n" + `  <guide><reference type="cover" href="cover.xhtml" title="Cover"/></guide>`
	}
	return PackageDoc(`<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf"` + opfNS + ` version="` + version + `" unique-identifier="pub-id"` + attrs + `>
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + strings.TrimRight(required(p.Meta)+p.Meta, "\n") + `
  </metadata>
  <manifest>
    ` + manifest + `
  </manifest>
  <spine` + spineAttr + `>` + spine + `<itemref idref="ch1"/></spine>` + guide + `
</package>`)
}

// The bytes the fixture archive's two content entries carry. CoverBytes is
// asserted against wherever a test proves a cover was left alone.
var (
	ChapterBytes = []byte("<html><body><p>chapter one</p></body></html>")
	CoverBytes   = []byte("ORIGINAL-COVER-BYTES")
)

// BaseEntries is the standard five-entry archive around a package document,
// plus whatever extra a test needs. One definition of what a fixture epub looks
// like.
func BaseEntries(opf PackageDoc, extra ...Entry) []Entry {
	es := []Entry{
		{Name: MimetypePath, Data: []byte(MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(ContainerXML)},
		{Name: OPFPath, Data: []byte(string(opf))},
		{Name: "OEBPS/cover.jpg", Data: CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: ChapterBytes},
	}
	return append(es, extra...)
}

// A package must carry an identifier, a title and a language (EPUB 3.3 §3.4.3),
// and only four tests in these suites are about any of them. These values are
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
// test needs that today: the parse tests reject a bad archive through a layer
// below this one.
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

// EPUB3 and EPUB2 are the common case: a package that varies only in its
// metadata. A test varying anything else writes a Pkg literal.
func EPUB3(meta string) PackageDoc { return Pkg{Meta: meta}.EPUB3() }
func EPUB2(meta string) PackageDoc { return Pkg{Meta: meta}.EPUB2() }

// Build writes the standard five-entry archive around a package document and
// returns its path.
func Build(t *testing.T, opf PackageDoc) string {
	t.Helper()
	return WriteEpub(t, BaseEntries(opf))
}
