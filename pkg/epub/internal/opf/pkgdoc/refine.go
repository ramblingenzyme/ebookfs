package pkgdoc

import (
	"slices"
	"strings"

	"github.com/beevik/etree"
)

// refinesID reports whether m refines the element with id. Under §5.3.6 both
// "#c1" and "content.opf#c1" target c1. An empty id matches nothing, or every
// meta without a refines would.
//
// ponytail: a path naming another document still binds to a local id of that
// name. Resolve the URL against the package document's own name if a real
// epub refines across files.
func refinesID(m *etree.Element, id string) bool {
	if id == "" {
		return false
	}
	_, frag, ok := strings.Cut(attr(m, "refines"), "#")
	return ok && frag == id
}

func (d *Doc) refineElements(id, property string) []*etree.Element {
	var out []*etree.Element
	for _, m := range d.md.children("meta") {
		if d.vocab.Same(attr(m, "property"), property) && refinesID(m, id) {
			out = append(out, m)
		}
	}
	return out
}

// addRefine spells property and scheme for this document. It makes two choices
// §5.3.6 leaves open: the meta goes at the end rather than beside its element,
// since the binding is by id, and refines is fragment-only.
func (d *Doc) addRefine(id, property, value, scheme string) {
	m := d.md.metaParent().CreateElement("meta")
	m.CreateAttr("refines", "#"+id)
	m.CreateAttr("property", d.vocab.spell(property))
	if scheme != "" {
		m.CreateAttr("scheme", d.vocab.spell(scheme))
	}
	m.SetText(value)
}

func (d *Doc) removeRefinements(ids []string) {
	for _, m := range d.md.children("meta") {
		if slices.ContainsFunc(ids, func(id string) bool { return refinesID(m, id) }) {
			detach(m)
		}
	}
}
