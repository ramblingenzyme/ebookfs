package opf

import (
	"strings"

	"github.com/beevik/etree"
)

// Reading <meta> elements in both of the encodings a package document uses:
// EPUB 3 refinements, which attach a property to another element by id, and the
// EPUB 2 name/content pairs that predate them.

// refTarget returns the id a refines attribute points at. Per §5.3.6 both "#c1"
// and "content.opf#c1" target c1, and a value with no fragment refines the
// document rather than an element in it.
//
// ponytail: a path naming a different document binds to a same-named local id
// instead of failing. Revisit if a real epub refines across files; the fix is
// resolving the relative URL against the OPF's own name.
func refTarget(v string) string {
	_, frag, ok := strings.Cut(collapse(v), "#")
	if !ok {
		return ""
	}
	return frag
}

// refinesID matches nothing for an empty id: every meta without a refines
// attribute would otherwise compare equal to it.
func refinesID(m *etree.Element, id string) bool {
	return id != "" && refTarget(m.SelectAttrValue("refines", "")) == id
}

// prop is a meta property from the EPUB 3 vocabulary, together with the rule
// for deciding which refinements carry its value.
type prop struct {
	name string
	// unschemed restricts matching to refinements carrying no scheme attribute.
	// D.3.4 defines series and set only when no scheme is specified; a value
	// from someone else's code list is not ours to read or overwrite.
	unschemed bool
}

var (
	propFileAs         = prop{name: "file-as"}
	propRole           = prop{name: "role"}
	propGroupPosition  = prop{name: "group-position"}
	propCollectionType = prop{name: "collection-type", unschemed: true}
)

// refinements returns every meta refining id that carries p's value. Plural
// because the vocabulary allows it: role is "zero or more" (D.3.10).
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

func (o *Doc) refine(id string, p prop) string {
	if ms := o.refinements(id, p); len(ms) > 0 {
		return text(ms[0])
	}
	return ""
}
