package opf

import (
	"strings"

	"github.com/beevik/etree"
)

// Reading <meta> elements, in both of the encodings a package document uses:
// EPUB 3 refinements, which attach a property to another element by id, and the
// EPUB 2 name/content pairs that predate them.

// --- EPUB 3 refinements ------------------------------------------------------

// refTarget returns the id a refines attribute points at. §5.3.6: "EPUB
// creators MUST use as the value a path-relative-scheme-less-URL string,
// optionally followed by U+0023 (#) and a URL-fragment string" — so both "#c1"
// and "content.opf#c1" target c1, and a value with no fragment refines the
// document rather than an element in it.
//
// ponytail: a path naming a *different* document is resolved as if it were
// local, so a cross-document refines silently binds to a same-named local id
// instead of failing. We neither produce nor need cross-document refinement.
// Revisit if a real epub turns up whose refines path names another file — the
// fix is resolving the relative URL against the OPF's own name.
func refTarget(v string) string {
	_, frag, ok := strings.Cut(collapse(v), "#")
	if !ok {
		return ""
	}
	return frag
}

// refinesID reports whether m is a refinement of the element with the given id.
// An empty id matches nothing — every meta without a refines attribute would
// otherwise compare equal to it.
func refinesID(m *etree.Element, id string) bool {
	return id != "" && refTarget(m.SelectAttrValue("refines", "")) == id
}

// prop is a meta property from the EPUB 3 vocabulary, together with the rule
// for deciding which refinements carry its value.
//
// It exists so that rule cannot be stated twice. Every read and every write of
// a property goes through refinements(id, prop), so the reader and the writer
// physically cannot resolve the same property to different elements — the
// failure this package keeps producing: a rename once duplicated the series
// because the writer could not see a collection the reader could not see, and a
// second appeared the moment collection-type grew a scheme rule the reader
// honoured and the writer did not.
type prop struct {
	name string
	// unschemed restricts matching to refinements carrying no scheme attribute.
	// D.3.4: "When the collection-type value is drawn from a code list or other
	// formal enumeration, EPUB creators SHOULD attach a scheme attribute to
	// identify its source. This specification also defines the following
	// collection types when no scheme is specified: series / set." A value under
	// someone else's code list is neither ours to read as a series nor ours to
	// overwrite.
	unschemed bool
}

var (
	propFileAs         = prop{name: "file-as"}
	propRole           = prop{name: "role"}
	propGroupPosition  = prop{name: "group-position"}
	propCollectionType = prop{name: "collection-type", unschemed: true}
)

// refinements returns every meta refining id that carries p's value. Plural
// because the vocabulary allows it: D.3.10 gives role cardinality "zero or
// more", and Example 92 gives one dc:creator both aut and ill.
func (o *Doc) refinements(id string, p prop) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if attr(m, "property") != p.name || !refinesID(m, id) {
			continue
		}
		if p.unschemed && attr(m, "scheme") != "" {
			continue
		}
		out = append(out, m)
	}
	return out
}

// refine returns the value of the first refinement of id carrying p, or "" if
// there is none.
func (o *Doc) refine(id string, p prop) string {
	if ms := o.refinements(id, p); len(ms) > 0 {
		return text(ms[0])
	}
	return ""
}
