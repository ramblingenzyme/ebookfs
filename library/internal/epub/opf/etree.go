package opf

import (
	"strconv"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The whole etree-facing surface: everything else goes through these.

// text is nil-safe: primary returns nil for an absent element and every caller
// wants "" for that.
func text(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return xml.Collapse(e.Text())
}

// attr collapses: etree wraps encoding/xml, which does not apply XML 1.0
// §3.3.3, so a tab or newline in an attribute value arrives verbatim.
func attr(e *etree.Element, name string) string {
	return xml.Collapse(e.SelectAttrValue(name, ""))
}

// detach removes an element from wherever it sits: the parent may be a
// dc-metadata or x-metadata wrapper rather than <metadata>.
func detach(e *etree.Element) {
	if p := e.Parent(); p != nil {
		p.RemoveChild(e)
	}
}

// qualify joins an xmlns: prefix to a local name — never a vocabulary prefix,
// which is vocab.go's spell.
func qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}

// ensureID returns the element's id, minting "stem", "stem-2", … if it has
// none. Uniqueness is checked against every id in the document, not just those
// of the same kind: XML 1.0 §3.3.1 makes ID values unique document-wide.
//
// stem is a name to build an id from, not a prefix in either sense the rest of
// this package uses the word: not an xmlns: prefix, not a vocabulary prefix.
//
// ponytail: rescans per call, O(n²) over a document holding tens of elements.
// Thread a set through the callers only if a profile ever says to.
func (o *Doc) ensureID(el *etree.Element, stem string) string {
	if id := attr(el, "id"); id != "" {
		return id
	}
	taken := map[string]bool{}
	for _, e := range o.pkg.FindElements("//*[@id]") {
		taken[attr(e, "id")] = true
	}
	id := stem
	for n := 2; taken[id]; n++ {
		id = stem + "-" + strconv.Itoa(n)
	}
	el.CreateAttr("id", id)
	return id
}
