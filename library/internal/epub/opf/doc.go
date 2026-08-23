// Package opf reads and writes the EPUB package document: the .opf file holding
// a book's metadata. The zip container around it belongs to the parent epub
// package.
//
// A field is one piece of metadata ebookfs owns. Each holds that field's whole
// encoding, and reading (get) and writing (set) both go through it so the two
// cannot disagree. Book-level validation and presentation defaults
// stay out of the fields and live in Bib, so set(get()) never invents metadata
// the file did not carry.
//
// Three rules keep the fields readable:
//
//   - A field with a write side is a type with get/set (title, authors, series,
//     modified). A read-only field is a single Doc method (description,
//     language, pubdate, identifiers, cover).
//
//   - A field says what a value should be and never where it is kept. Slots
//     (slot.go) know where: an element's text, a refinement, an opf: attribute,
//     a named meta. Under both sit the finders — metadata.go for the children of
//     <metadata>, the parents new ones belong in, and the xmlns: prefix to give
//     them; refine.go for the EPUB 3 refinement binding; vocab.go for the
//     vocabulary a property name resolves in — and under everything, the
//     normalization in etree.go that every value this package hands out passes
//     through.
//
//     The package has two naming systems and they are not the same thing.
//     metadata.go handles XML namespaces, declared with xmlns: and resolved by
//     the parser; vocab.go handles property vocabularies, declared with the
//     package element's prefix attribute and living inside attribute values.
//     Each answers the same question for its own system — give me a prefix bound
//     to this, declaring one if the document has none — which is ensureOPFPrefix
//     on one side and spell/declarePrefix on the other.
//
//   - The EPUB 2 / EPUB 3 branch stays visible in each field. The two specs
//     genuinely differ, and for the sort title v2 has no mechanism at all;
//     hiding that behind a common writer would hide what matters.
package opf

import (
	"bytes"
	"errors"
	"strings"
	"time"

	"github.com/beevik/etree"
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
//
// It goes through attr for the same reason everything else does: a wrapped or
// padded version would otherwise read as EPUB 2, and getting this one wrong
// costs the §5.5.5 dcterms:modified update and injects calibre metas into a
// package that never had any. A file with no version attribute at all is
// malformed either way, and EPUB 2 is the safer guess for one.
func (o *Doc) epub3() bool {
	return strings.HasPrefix(attr(o.pkg, "version"), "3")
}

// Apply writes the edits into the document and reports whether that changed
// anything; nothing is serialized until Bytes. A false means the file already
// said what the edit asked for, so the caller has nothing to write back.
//
// etree is used rather than encoding/xml because it round-trips namespace
// declarations, dc: prefixes, comments and formatting untouched.
func (o *Doc) Apply(e model.Edits) bool {
	before, _ := o.Bytes()

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

	// §5.5.5 asks for the timestamp when the creator makes changes, so an edit
	// that asks for what the file already says is not one. Comparing the whole
	// serialization is the only honest test of that: a field's set is free to
	// decide the document already carries the value, and only the bytes know.
	if after, _ := o.Bytes(); bytes.Equal(before, after) {
		return false
	}
	o.modified().set(time.Now())
	return true
}

// Bib reads the book's metadata out of the document, adding what is not a
// field's business: whole-book validation and presentation defaults. base is the
// OPF's own directory, needed only to resolve the cover href.
func (o *Doc) Bib(base string) (*model.Bib, error) {
	b := &model.Bib{}

	// Reported as written. §5.5.2 licenses stripping and collapsing whitespace,
	// which get already did, and nothing else: a value is text, not a path
	// component. Making it safe to use as one is the business of whoever builds
	// the path — model.PathSafe, called by the store and the 9P names.
	title, sortTitle := o.title().get()
	if title == "" {
		return nil, errors.New("no title")
	}
	b.Title = title
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
