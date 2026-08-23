package opf

import (
	"strconv"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The whole etree-facing surface. Everything else in this package reads and
// writes the document through these.

// text is nil-safe: primary returns nil for an absent element and every caller
// wants "" for that.
func text(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return xml.Collapse(e.Text())
}

// attr collapses too. Nothing upstream does it for us: XML 1.0 §3.3.3 would
// normalize a newline in an attribute value to a space, but encoding/xml, which
// etree wraps, does not implement it, so a tab or newline arrives verbatim.
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

// qualify joins an XML namespace prefix to a local name: the dcPrefix or
// ensureOPFPrefix kind, never a vocabulary prefix. vocab.go's spell is the
// vocabulary-side twin.
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
