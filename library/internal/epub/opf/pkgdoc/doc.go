// Package pkgdoc is the XML side of the EPUB package document: parsing it,
// finding the elements a value can be kept in, and writing values back. What a
// value should be is the parent opf package's business.
//
// A slot (slot.go) is one place a value is kept — element text, a refinement,
// an opf: attribute, a named meta. Under them sit the finders: metadata.go for
// the children of <metadata>, refine.go for the EPUB 3 refinement binding, and
// the two naming systems anything new has to be spelled in — ns.go for xmlns:
// prefixes, vocab.go for vocabulary ones. ns says how those two differ.
package pkgdoc

import (
	"errors"
	"strings"

	"github.com/beevik/etree"
)

type Doc struct {
	doc   *etree.Document
	pkg   *etree.Element // <package>
	md    metadata       // the children of <metadata>
	ns    ns             // xmlns: prefixes
	vocab vocab          // vocabulary prefixes
}

// Parse reads the package document. etree is used rather than encoding/xml
// because it round-trips namespace declarations, dc: prefixes, comments and
// formatting untouched.
func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
	// A CDATA section is a spelling of a value, not a different value: a
	// description wrapped in one reads the same either way. Preserving it keeps
	// an edit from rewriting the spelling of a field it was not asked to touch.
	doc.ReadSettings.PreserveCData = true
	if err := doc.ReadFromBytes(b); err != nil {
		return nil, err
	}
	pkg := doc.SelectElement("package")
	if pkg == nil {
		return nil, errors.New("opf: no <package> element")
	}
	md := pkg.SelectElement("metadata")
	if md == nil {
		return nil, errors.New("opf: no <metadata> element")
	}
	return &Doc{doc: doc, pkg: pkg, md: metadata{md}, ns: ns{pkg}, vocab: vocab{pkg}}, nil
}

func (d *Doc) Bytes() ([]byte, error) { return d.doc.WriteToBytes() }

// EPUB3 decides how metadata is written: refinements for v3, opf: attributes and
// calibre metas for v2. Through attr, since a padded version would otherwise read
// as EPUB 2 — which costs the §5.5.5 dcterms:modified update and injects calibre
// metas. No version attribute at all is malformed; EPUB 2 is the safer guess.
func (d *Doc) EPUB3() bool {
	return strings.HasPrefix(attr(d.pkg, "version"), "3")
}

// HasProperty reports whether a property list contains one. §5.9.1 makes it "a
// space-separated list of property values", so membership is a token comparison:
// a substring test would match my-cover-image, someone else's property.
func (d *Doc) HasProperty(list, want string) bool { return d.vocab.has(list, want) }
