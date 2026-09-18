// The complete documents these tests share, kept as files so they read as XML
// and diff as XML, and the XML snippets that go into a document built around
// them. Each file here is used whole; a document that varies a single element
// stays inline beside the test that varies it.
//
// The whole directory is embedded rather than each file, so adding a document
// is adding a file.

package epubtest

import (
	"embed"
	"strings"
)

//go:embed testdata
var fixtures embed.FS

// packageFixture is fixture for the documents that are package documents, so
// they carry the methods rather than needing a conversion at every use.
func packageFixture(name string) PackageDoc { return PackageDoc(fixture(name)) }

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
	OPF3        = packageFixture("package-epub3.opf")
	OPF2        = packageFixture("package-epub2.opf")
	RichOPF3    = packageFixture("rich-epub3.opf")
	RichOPF2    = packageFixture("rich-epub2.opf")
	NCXOPF      = packageFixture("ncx-package.opf")
	OPFWrappers = packageFixture("legacy-wrappers.opf")

	// DCMetadataOnly has dc-metadata present and x-metadata absent: §2.2's MUST
	// still binds what an edit adds, and this is the one package a reader cannot
	// see violating it.
	DCMetadataOnly = packageFixture("dc-metadata-only.opf")

	// SVGCoverPage is the shape calibre and Sigil both produce.
	SVGCoverPage = fixture("svg-cover-page.xhtml")

	// MultiRootContainer declares two package rootfiles where the first does not
	// exist in the zip, the shape seen in some Kobo epubs. It has no builder
	// because two rootfiles is the shape being tested, not a value inside one.
	MultiRootContainer = fixture("container-multi-root.xml")
)

// ContainerXML is META-INF/container.xml for the standard fixture layout.
var ContainerXML = ContainerFor(OPFPath, PackageMediaType)

// Metas joins metadata parts into a <metadata> body, indenting each line, so a
// fixture composes from named pieces instead of splicing calls into a backtick
// string. A raw XML line is a valid part, which is what lets a fixture name the
// pieces it is about and write out the one it is not.
func Metas(parts ...string) string {
	var out []string
	for _, part := range parts {
		for line := range strings.SplitSeq(part, "\n") {
			if line == "" {
				out = append(out, "")
				continue
			}
			out = append(out, "    "+line)
		}
	}
	return strings.Join(out, "\n")
}

// Collection is the EPUB 3 series encoding: the collection, the collection-type
// refinement saying what kind it is, and a group-position when the book has one
// (§5.5.3.3, D.3.3). An empty position is a book in a series with no stated place
// in it. parent nests this collection inside another, which D.3.3 allows and
// which only a series inside a set uses.
func Collection(id, name, kind, position string, parent ...string) string {
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

// CalibreSeries is the EPUB 2 encoding of the same thing, the pair of metas
// calibre writes. An empty index omits calibre:series_index, which is what a
// series carrying no position looks like on the way out.
func CalibreSeries(name, index string) string {
	out := `<meta name="calibre:series" content="` + name + `"/>`
	if index != "" {
		out += "\n" + `<meta name="calibre:series_index" content="` + index + `"/>`
	}
	return out
}

// PackageMediaType is how a reader decides a rootfile is the package document
// (OCF 3.3 §4.2.1). Declared here rather than reached for in epub, since these
// tests drive it from the outside and the spec fixes the value.
const PackageMediaType = "application/oebps-package+xml"

// Wrapped is an attribute value split across lines the way an editor breaks a
// long one. The newlines are the subject: XML 1.0 §3.3.3 turns each into a
// space, so the value arrives padded.
func Wrapped(value string) string { return "\n        " + value + "\n      " }

// The two encryption algorithms these tests distinguish: one that means the
// entry is really encrypted, one that means it is only font-obfuscated.
const (
	AES256     = "http://www.w3.org/2001/04/xmlenc#aes256-cbc"
	FontObfusc = "http://www.idpf.org/2008/embedding"
)

// EncryptionXML is META-INF/encryption.xml naming one encrypted resource. The
// two arguments are what the tests vary: the algorithm says whether the file is
// really encrypted or only font-obfuscated (OCF 3.3 §4.5), and the URI says
// which entry it covers.
func EncryptionXML(algorithm, uri string) string {
	return `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="` + algorithm + `"/>
    <enc:CipherData><enc:CipherReference URI="` + uri + `"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
}

// ContainerFor is META-INF/container.xml naming one rootfile. Both arguments are
// what the tests vary: the path, which may be percent-encoded or name an entry
// that is not in the archive, and the media type, which decides whether the
// rootfile is recognised at all.
func ContainerFor(fullPath, mediaType string) string {
	return `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="` + fullPath + `" media-type="` + mediaType + `"/>
  </rootfiles>
</container>`
}
