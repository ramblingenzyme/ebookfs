package opf

import (
	"slices"
	"strconv"

	"github.com/beevik/etree"
)

// setRefine updates the refinement already there rather than replacing it, so
// it keeps its position in the document.
//
// ponytail: duplicates of one property are left in place and only the first is
// updated. Revisit if epubcheck rejects a file we wrote.
func (o *Doc) setRefine(id string, p prop, value, scheme string) {
	ms := o.refinements(id, p)
	if len(ms) == 0 {
		o.addRefine(id, p, value, scheme)
		return
	}
	ms[0].SetText(value)
	if scheme != "" {
		ms[0].CreateAttr("scheme", scheme)
	}
}

// addRefine appends unconditionally, for properties where an existing value may
// be one we do not own, such as a creator's second role.
func (o *Doc) addRefine(id string, p prop, value, scheme string) {
	m := o.metaHome().CreateElement("meta")
	m.CreateAttr("refines", "#"+id)
	m.CreateAttr("property", p.name)
	if scheme != "" {
		m.CreateAttr("scheme", scheme)
	}
	m.SetText(value)
}

func (o *Doc) removeRefine(id string, p prop) {
	for _, m := range o.refinements(id, p) {
		detach(m)
	}
}

func refinesAny(m *etree.Element, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return refinesID(m, id) })
}

func (o *Doc) removeMetas(predicate func(*etree.Element) bool) {
	for _, m := range o.elements("meta") {
		if predicate(m) {
			detach(m)
		}
	}
}

// detach removes an element from wherever it sits: the parent may be a
// dc-metadata or x-metadata wrapper rather than <metadata>.
func detach(e *etree.Element) {
	if p := e.Parent(); p != nil {
		p.RemoveChild(e)
	}
}

// ensureID returns the element's id, minting "prefix", "prefix-2", … if it has
// none. Uniqueness is checked against every id in the document, not just those
// of the same kind: XML 1.0 §3.3.1 makes ID values unique document-wide.
//
// ponytail: rescans per call, O(n²) over a document holding tens of elements.
// Thread a set through the callers only if a profile ever says to.
func (o *Doc) ensureID(el *etree.Element, prefix string) string {
	if id := attr(el, "id"); id != "" {
		return id
	}
	taken := map[string]bool{}
	for _, e := range o.pkg.FindElements("//*[@id]") {
		taken[attr(e, "id")] = true
	}
	id := prefix
	for n := 2; taken[id]; n++ {
		id = prefix + "-" + strconv.Itoa(n)
	}
	el.CreateAttr("id", id)
	return id
}

func (o *Doc) setDCText(tag, value string) {
	if el := o.target(tag); el != nil {
		el.SetText(value)
		return
	}
	o.dcHome().CreateElement(qualify(o.dcPrefix(), tag)).SetText(value)
}

// dcPrefix takes the Dublin Core prefix from the always-present <dc:title>, so
// a new dc element matches the existing declaration. Defaults to "dc".
func (o *Doc) dcPrefix() string {
	if els := o.elements("title"); len(els) > 0 {
		return els[0].Space
	}
	return "dc"
}

func qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}
