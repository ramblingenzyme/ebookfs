package opf

import (
	"slices"
	"strconv"

	"github.com/beevik/etree"
)

// --- refinements -------------------------------------------------------------

// setRefine writes an EPUB 3 refinement of id under property, updating the one
// already there rather than replacing it, so it keeps its position in the
// document.
//
// ponytail: a malformed file carrying several refinements of the same property
// keeps them all and only the first is updated, so the others go on
// contradicting it. D.3.6's "Cardinality: zero or one" makes the duplicates the
// file's bug, and removing every one to append a replacement is the idiom this
// model exists to avoid. Revisit if epubcheck rejects a file we wrote, or if a
// duplicate refinement is seen to survive an edit that should have changed it.
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

// addRefine appends a refinement unconditionally. Callers that must not disturb
// an existing value of the same property use this directly — a creator's role,
// where D.3.10's "zero or more" means the one already there may be a different
// role we do not own.
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

// refinesAny reports whether m refines any of the given ids.
func refinesAny(m *etree.Element, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return refinesID(m, id) })
}

// --- EPUB 2 named metas ------------------------------------------------------

// --- removal -----------------------------------------------------------------

// removeMetas removes the meta elements matching the predicate.
func (o *Doc) removeMetas(predicate func(*etree.Element) bool) {
	for _, m := range o.elements("meta") {
		if predicate(m) {
			detach(m)
		}
	}
}

// detach removes an element from wherever it sits. The metadata children may be
// inside a dc-metadata or x-metadata wrapper, so the parent is not always
// <metadata>. etree nils a removed token's parent, which is what lets a caller
// tell afterwards whether an element it detached ever went back.
func detach(e *etree.Element) {
	if p := e.Parent(); p != nil {
		p.RemoveChild(e)
	}
}

// --- ids ---------------------------------------------------------------------

// ensureID returns the element's id, minting one from prefix if it has none:
// "prefix", then "prefix-2", "prefix-3", and so on until one is free. Refining
// an element requires it to have an id, so anything we attach a refinement to
// goes through here.
//
// The check is against every id in the document, not just those of the
// element's own kind: XML 1.0 §3.3.1 makes ID values unique document-wide, and
// two elements answering to one id would mean a refinement pointing at both.
//
// ponytail: rescans the document per call, which is O(n²) over a package
// document holding tens of elements. Thread a set through the callers only if a
// profile ever says to.
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

// --- element placement -------------------------------------------------------

// setDCText sets the text of a Dublin Core element, creating it if absent.
func (o *Doc) setDCText(tag, value string) {
	if el := o.target(tag); el != nil {
		el.SetText(value)
		return
	}
	o.dcHome().CreateElement(qualify(o.dcPrefix(), tag)).SetText(value)
}

// --- namespaces --------------------------------------------------------------

// dcPrefix returns the namespace prefix the document uses for Dublin Core
// elements, inferred from the always-present <dc:title>, so any new dc element
// we create matches the existing declaration. Defaults to "dc".
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
