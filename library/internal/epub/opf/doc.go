// Package opf reads and writes the EPUB package document: the .opf file holding
// a book's metadata. The zip container around it belongs to the parent epub
// package.
//
// A field is one piece of metadata ebookfs owns, one per file, named
// field_*.go. Each holds that field's whole encoding, and reading (get) and
// writing (set) both go through it so the two cannot disagree. Book-level
// validation and presentation defaults stay out of the fields and live in Bib,
// so set(get()) never invents metadata the file did not carry.
//
// Three rules keep the fields readable:
//
//   - A field with a write side is a type with get/set (title, authors, series,
//     modified). A read-only field is a single Doc method (description,
//     language, pubdate, identifiers, cover).
//   - Fields go through the named operations in dc.go, refine.go and epub2.go —
//     one file per encoding, holding both its read and its write half. Only
//     etree.go touches etree directly.
//   - The EPUB 2 / EPUB 3 branch stays visible in each field. The two specs
//     genuinely differ, and for the sort title v2 has no mechanism at all;
//     hiding that behind a common writer would hide what matters.
package opf

import (
	"errors"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type Doc struct {
	doc *etree.Document
	pkg *etree.Element // <package>
	md  *etree.Element // <metadata>
}

func Parse(b []byte) (*Doc, error) {
	doc := etree.NewDocument()
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
	return &Doc{doc: doc, pkg: pkg, md: md}, nil
}

func (o *Doc) Bytes() ([]byte, error) { return o.doc.WriteToBytes() }

// epub3 decides how metadata is written: refinements for v3, opf: attributes
// and calibre metas for v2. The version attribute is "3.0"/"3.1"/"2.0".
func (o *Doc) epub3() bool {
	return strings.HasPrefix(o.pkg.SelectAttrValue("version", ""), "3")
}

// Apply writes the edits into the document; nothing is serialized until Bytes.
// etree is used rather than encoding/xml because it round-trips namespace
// declarations, dc: prefixes, comments and formatting untouched.
func (o *Doc) Apply(e model.Edits) {
	o.title().set(e.Title, e.SortTitle)

	if e.Description != nil {
		o.dcElement("description", "").set(*e.Description)
	}
	if e.Language != nil {
		o.dcElement("language", "").set(*e.Language)
	}
	if e.Authors != nil {
		o.authors().set(*e.Authors)
	}
	if e.Series != nil || e.SeriesIndex != nil {
		o.series().set(e.Series, e.SeriesIndex)
	}

	o.modified().set(time.Now())
}

// Bib reads the book's metadata out of the document, adding what is not a
// field's business: whole-book validation and presentation defaults. base is the
// OPF's own directory, needed only to resolve the cover href.
func (o *Doc) Bib(base string) (*model.Bib, error) {
	b := &model.Bib{}

	title, sortTitle := o.title().get()
	if title == "" {
		return nil, errors.New("no title")
	}
	var err error
	if b.Title, err = naming.Sanitize(title); err != nil {
		return nil, errors.New("empty title")
	}
	// REVIEW: we don't sanitize sortTitle?
	b.SortTitle = sortTitle
	// TODO: decide whether to derive a sort title heuristically when none is set
	// (calibre strips leading articles, e.g. "The Hobbit" -> "Hobbit, The"); it is
	// language-dependent, so for now an unset sort title is left empty.

	if b.Authors = o.authors().get(); len(b.Authors) == 0 {
		return nil, errors.New("no authors")
	}

	if s := o.series().get(); s != nil {
		// Defaulted here, not in the field, so a rewrite cannot write it back.
		if !model.ValidSeriesIndex(s.Index) {
			s.Index = "1"
		}
		b.Series = s
	}

	b.Description = o.description()
	b.Language = o.language()
	b.Pubdate = o.pubdate()
	b.Identifiers = o.identifiers()
	b.CoverPath = o.cover(base)

	return b, nil
}
