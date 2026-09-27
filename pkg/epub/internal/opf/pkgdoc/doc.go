// Package pkgdoc is the XML side of the EPUB package document: parsing it,
// finding the elements a value is kept in, and writing values back. What a
// value should be is package opf's business.
package pkgdoc

import (
	"errors"
	"strings"

	"github.com/beevik/etree"
)

type Doc struct {
	doc   *etree.Document
	pkg   *etree.Element // <package>
	md    metadata
	ns    ns
	vocab vocab
}

// Parse reads the package document. It uses etree rather than encoding/xml
// because etree round-trips namespace declarations, dc: prefixes, comments and
// formatting.
//
// It does not set ValidateInput, unlike ncx.Parse and content.Parse. A package
// document that fails to parse loses the whole book, while those two only skip
// their file, so for them rejecting a malformed document beats letting etree
// correct it.
func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
	// CDATA is a spelling of a value, not a different value, so an edit keeps
	// it rather than re-encoding a field it was not asked to touch.
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

// EPUB3 reports whether the package is EPUB 3, which decides how metadata is
// written. It reads version through attr, since a padded "3.0" read as EPUB 2
// would skip the §5.5.5 update and add calibre metas. A missing version is
// malformed, and EPUB 2 is the safer guess.
func (d *Doc) EPUB3() bool {
	return strings.HasPrefix(attr(d.pkg, "version"), "3")
}

func (d *Doc) HasProperty(list, want string) bool { return d.vocab.has(list, want) }
