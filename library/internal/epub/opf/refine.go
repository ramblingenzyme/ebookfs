package opf

import (
	"slices"
	"strings"

	"github.com/beevik/etree"
)

// EPUB 3 refinements: a <meta property="..."> carrying a value for another
// element, bound to it by id. The EPUB 2 name/content encoding that predates
// them is in epub2.go.

// refinesID reports whether m refines the element with the given id. Per §5.3.6
// both "#c1" and "content.opf#c1" target c1, and a value with no fragment
// refines the document rather than an element in it. An empty id matches
// nothing: every meta without a refines attribute would otherwise equal it.
//
// ponytail: a path naming a different document binds to a same-named local id
// instead of failing. Revisit if a real epub refines across files; the fix is
// resolving the relative URL against the OPF's own name.
func refinesID(m *etree.Element, id string) bool {
	if id == "" {
		return false
	}
	_, frag, ok := strings.Cut(Collapse(m.SelectAttrValue("refines", "")), "#")
	return ok && frag == id
}

// refineElements returns every meta refining id that carries the property.
// Plural because the vocabulary allows it: role is "zero or more" (D.3.10).
//
// A property whose meaning depends on the scheme attribute — collection-type,
// per D.3.4 — is filtered by its own field, not here.
func (o *Doc) refineElements(id, property string) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if o.sameProperty(attr(m, "property"), property) && refinesID(m, id) {
			out = append(out, m)
		}
	}
	return out
}

// addRefine appends unconditionally, for properties where an existing value may
// be one we do not own, such as a creator's second role.
// Both property and scheme go through spell: each is a prefixed name, and a
// document that rebound the vocabulary either resolves in would otherwise turn
// our value into one from somebody else's. scheme matters most today — role refinements
// carry marc:relators — since every property we write is default-vocabulary.
func (o *Doc) addRefine(id, property, value, scheme string) {
	m := o.metaParent().CreateElement("meta")
	m.CreateAttr("refines", "#"+id)
	m.CreateAttr("property", o.spell(property))
	if scheme != "" {
		m.CreateAttr("scheme", o.spell(scheme))
	}
	m.SetText(value)
}

// removeRefinements drops every meta refining any of ids, for use once the
// elements they refine are themselves gone.
func (o *Doc) removeRefinements(ids []string) {
	for _, m := range o.elements("meta") {
		if slices.ContainsFunc(ids, func(id string) bool { return refinesID(m, id) }) {
			detach(m)
		}
	}
}
