// Package epubtest builds the epub corpus both epub test suites drive: the
// archive on disk, the package document inside it, and the assertions that read
// one back. The public epub package tests the format; library/internal/epub
// tests the translation to the library's model. Both need the same fixtures, and
// neither can reach the other's test files.
//
// A document assembled from named parts is built here; a complete one kept as
// a file is in fixtures.go.
package epubtest

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	OPFPath = "OEBPS/content.opf"

	// The OCF names, both defined by §4.3.3.
	MimetypePath  = "mimetype"
	MimetypeValue = "application/epub+zip"
)

type Entry struct {
	Name  string
	Data  []byte
	Store bool // stored (uncompressed) rather than deflated
}

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

const ChapterOnlyManifest = `<item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`

type PackageDoc string

// With lets a test vary one <meta> on a complete fixture without restating the
// whole document.
func (d PackageDoc) With(parts ...string) PackageDoc {
	const closeTag = "  </metadata>"
	before, after, ok := strings.Cut(string(d), closeTag)
	if !ok {
		panic("package document has no </metadata>")
	}
	return PackageDoc(before + strings.Join(parts, "\n") + "\n" + closeTag + after)
}

// Pkg is a package document described by its parts. Every field but Meta has a
// default, so the document is built once rather than built and then patched.
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

var (
	ChapterBytes = []byte("<html><body><p>chapter one</p></body></html>")
	CoverBytes   = []byte("ORIGINAL-COVER-BYTES")
)

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

// A package must carry an identifier, a title and a language (EPUB 3.3 §3.4.3);
// a creator rides along because most fixtures want one. These values are
// deliberately uninteresting, so seeing one in a failure message means the test
// was not about it.
const (
	fillIdentifier = `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>`
	fillTitle      = `    <dc:title>The Title</dc:title>`
	fillCreator    = `    <dc:creator id="c1">Ann Rand</dc:creator>`
	fillLanguage   = `    <dc:language>en</dc:language>`
)

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

func Build(t *testing.T, opf PackageDoc) string {
	t.Helper()
	return WriteEpub(t, BaseEntries(opf))
}
