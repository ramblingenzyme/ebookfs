// Package opf reads and writes the EPUB package document: the .opf file holding
// a book's metadata. The zip container around it belongs to the parent epub
// package.
//
// A field is one piece of metadata ebookfs owns, one per file, named
// field_*.go. Each holds that field's whole encoding, and reading (get) and
// writing (set) both go through it so the two cannot disagree. Book-level
// validation and presentation defaults stay out of the fields and live in Bib,
// so set(get()) never invents metadata the file did not carry.
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

// collapse is the whitespace normalization §5.5.2 requires of a reader. Every
// value this layer hands out passes through it.
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

func text(e *etree.Element) string { return collapse(e.Text()) }

// attr collapses too: XML 1.0 §3.3.3 turns a newline in an attribute value into
// a space without trimming it.
func attr(e *etree.Element, name string) string {
	return collapse(e.SelectAttrValue(name, ""))
}

// children flattens the deprecated OPF 2.0 §2.2 wrappers, so no field has to
// know about them. The write side is dcHome/metaHome in epub2.go.
func (o *Doc) children() []*etree.Element {
	var out []*etree.Element
	for _, c := range o.md.ChildElements() {
		switch c.Tag {
		case "dc-metadata", "x-metadata":
			out = append(out, c.ChildElements()...)
		default:
			out = append(out, c)
		}
	}
	return out
}

// elements matches on etree's Tag, which is the local name with the prefix held
// separately, so it matches whatever prefix the document binds Dublin Core to.
func (o *Doc) elements(tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range o.children() {
		if c.Tag == tag {
			out = append(out, c)
		}
	}
	return out
}

// first skips empty values: §5.5.2 requires non-empty ones, so an empty element
// is a malformed file and the usable value after it is preferred to no book.
func (o *Doc) first(tag string) (*etree.Element, string) {
	for _, e := range o.elements(tag) {
		if v := text(e); v != "" {
			return e, v
		}
	}
	return nil, ""
}

// target is the element a read and a write of a Dublin Core field both mean:
// the first with a non-empty value (§5.5.3.1.2 for the title), or the first
// present when they are all empty, since a write has to land somewhere.
func (o *Doc) target(tag string) *etree.Element {
	if el, _ := o.first(tag); el != nil {
		return el
	}
	if els := o.elements(tag); len(els) > 0 {
		return els[0]
	}
	return nil
}

func (o *Doc) manifest() []manifestItem {
	m := o.pkg.SelectElement("manifest")
	if m == nil {
		return nil
	}
	var out []manifestItem
	for _, it := range m.SelectElements("item") {
		out = append(out, manifestItem{
			ID: attr(it, "id"),
			// Only trimmed, not collapsed: href is a percent-encoded URL, and
			// collapsing could rewrite a literal filename.
			Href:       strings.TrimSpace(it.SelectAttrValue("href", "")),
			MediaType:  attr(it, "media-type"),
			Properties: attr(it, "properties"),
		})
	}
	return out
}

type manifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

// Apply writes the edits into the document; nothing is serialized until Bytes.
// etree is used rather than encoding/xml because it round-trips namespace
// declarations, dc: prefixes, comments and formatting untouched.
func (o *Doc) Apply(e model.Edits) {
	o.title().set(e.Title, e.SortTitle)

	if e.Description != nil {
		o.setDCText("description", *e.Description)
	}
	if e.Language != nil {
		o.setDCText("language", *e.Language)
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
