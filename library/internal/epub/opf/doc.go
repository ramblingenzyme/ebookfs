// Package opf reads and writes the EPUB package document — the .opf file
// holding a book's metadata. The zip container around it belongs to the parent
// epub package; everything here is the document and the spec rules that govern
// it, cited from specs/ throughout.
//
// The API across that boundary is deliberately four identifiers: Parse, Bib,
// Apply, Bytes. Everything else stays unexported, which is the point — helpers
// like collapse and text exist so that no caller reads raw chardata, and
// exporting them would invert their purpose.
//
// # Fields
//
// A field is one piece of metadata ebookfs owns. Each accessor on Doc returns a
// small value that knows that field's whole encoding — where it lives, which
// element wins when several could, and how EPUB 2 records it differently from
// EPUB 3 — and both directions go through it: get for the reader, set for the
// writer. One field per file, named field_*.go.
//
// This exists because of what kept going wrong without it. The reader and the
// writer each resolved "where does this book's series live" separately, and
// five bugs came out of the two answers disagreeing: a rename that duplicated
// the collection, a rename a v2 file ignored, a schemed collection-type
// overwritten, an index-only edit that deleted a series the reader could see,
// and creator ids that collided. Every one was fixed by making one side copy
// the other's rule, which works exactly until the next person edits one side.
//
// A field resolves once, so the two sides cannot hold different rules.
//
// The accessors are a convention, not a Go interface: the types genuinely
// differ — a title carries two values, modified is write-only, pubdate and the
// cover are read-only — and an interface over them would have to erase that to
// say nothing useful.
//
// What stays out of the fields: book-level validation ("no title", "no
// authors") and presentation defaults (a missing series position becoming "1"),
// both of which live in Bib. A field reports the document's truth, so that
// set(get()) can never invent metadata the file did not carry.
package opf

import (
	"errors"
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Doc is a parsed package document: the etree tree, plus the two elements every
// field starts from.
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

// epub3 reports whether the package is EPUB 3, which decides how metadata is
// written: refinements for v3, opf: attributes and calibre metas for v2. The
// version attribute is "3.0"/"3.1"/"2.0", so the major digit is the test.
func (o *Doc) epub3() bool {
	return strings.HasPrefix(o.pkg.SelectAttrValue("version", ""), "3")
}

// --- values ------------------------------------------------------------------

// collapse applies the normalization EPUB 3.3 §5.5.2 defines as a processing
// step, not as a constraint on the file: "Whitespace within these element
// values is not significant. Sequences of one or more whitespace characters are
// collapsed to a single space during processing."
//
// Every value this layer hands out passes through here, which is deliberate: a
// caller that reaches for raw chardata is how a document formatted the way the
// spec prints its own examples came to fail with "no authors".
func collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// text is an element's collapsed text content.
func text(e *etree.Element) string { return collapse(e.Text()) }

// attr is an element's collapsed attribute value. XML 1.0 §3.3.3 turns a
// newline inside an attribute value into a space but does not trim it, so the
// EPUB 2 attributes need collapsing just as much as element text does.
func attr(e *etree.Element, name string) string {
	return collapse(e.SelectAttrValue(name, ""))
}

// --- metadata children -------------------------------------------------------

// children returns the metadata element's child elements, looking through the
// deprecated wrappers. OPF 2.0 §2.2: "Reading Systems must allow the
// specification of the deprecated dc-metadata and x-metadata elements. ... If
// the dc-metadata element is used, all dc elements must go in dc-metadata and
// all other metadata elements, if any, must go into x-metadata."
//
// Flattening here rather than at each call site is what makes that MUST hold
// for every field at once. The wrappers are the only EPUB 2 concern left in
// this file — the helpers that write back into them are in epub2.go.
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

// elements returns the metadata children with the given local name. etree's Tag
// is the local name with the prefix held separately, so this matches whatever
// prefix the document binds Dublin Core to.
func (o *Doc) elements(tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range o.children() {
		if c.Tag == tag {
			out = append(out, c)
		}
	}
	return out
}

// first returns the first metadata child with the given local name whose text
// is non-empty once collapsed, along with that text. §5.5.2 requires non-empty
// values, so an empty one is a malformed file; skipping it recovers the usable
// value sitting right after it instead of rejecting the book.
func (o *Doc) first(tag string) (*etree.Element, string) {
	for _, e := range o.elements(tag) {
		if v := text(e); v != "" {
			return e, v
		}
	}
	return nil, ""
}

// --- manifest ----------------------------------------------------------------

// target returns the element a read and a write of a Dublin Core field both
// mean: the first with a non-empty value — for the title, §5.5.3.1.2's "The
// first dc:title element in document order is the main title of the EPUB
// publication" — falling back to the first present when they are all empty,
// since a write still has to land somewhere, and nil when there are none.
// Resolving a read and a write to the same element is the property that
// matters, whatever the file's own ordering says.
func (o *Doc) target(tag string) *etree.Element {
	if el, _ := o.first(tag); el != nil {
		return el
	}
	if els := o.elements(tag); len(els) > 0 {
		return els[0]
	}
	return nil
}

// manifest returns the manifest items, in document order.
func (o *Doc) manifest() []manifestItem {
	m := o.pkg.SelectElement("manifest")
	if m == nil {
		return nil
	}
	var out []manifestItem
	for _, it := range m.SelectElements("item") {
		out = append(out, manifestItem{
			ID: attr(it, "id"),
			// href is a URL and must be percent-encoded, so unlike every
			// other value here it is only trimmed: collapsing runs of
			// whitespace could rewrite a literal (non-conforming) filename.
			Href:       strings.TrimSpace(it.SelectAttrValue("href", "")),
			MediaType:  attr(it, "media-type"),
			Properties: attr(it, "properties"),
		})
	}
	return out
}

// manifestItem is a snapshot of a <manifest><item>. The manifest is read-only
// for us — only cover resolution consults it — so a flat value is easier to
// work with than the element.
type manifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

// Apply writes the edits into the document. Nothing is serialized until Bytes:
// etree round-trips XML without rewriting namespace declarations or mangling
// the dc: prefixes the way encoding/xml's encoder would, so untargeted nodes,
// comments and formatting survive untouched.
//
// Each field's set owns the whole of that field's encoding — including the "the
// edit did not name this" rule, since resolving it needs the value already in
// the file. See the field_*.go files.
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

// Bib reads the book's metadata out of the document. base is the OPF's own
// directory within the container, needed only to resolve the cover href.
//
// Every field translates itself; this assembles the result and adds the two
// things that are not a field's business — validation of the book as a whole,
// and the presentation defaults the frontend expects.
func (o *Doc) Bib(base string) (*model.Bib, error) {
	b := &model.Bib{}

	// §5.5.2 requires non-empty values, so a document whose only dc:title is
	// empty is invalid and the spec does not say what to do with one; the field
	// recovers the usable title that follows instead of rejecting the book.
	// Sanitize failing is different — the title is present but unusable — and
	// keeps its own error.
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
		// The field reports the document's truth, so defaulting a missing
		// position happens here: a rewrite must never invent a group-position
		// the file did not carry. The default is calibre's convention; a 0
		// would sort ahead of the real, numbered entries in the by-series view.
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
