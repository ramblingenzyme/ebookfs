package opf

import (
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// The whole etree-facing surface. Everything else in this package reads and
// writes the document through these.

// Collapse is the whitespace normalization §5.5.2 requires of a reader. Every
// value this layer hands out passes through it.
//
// Exported for the container attributes in the parent package: they decode
// through encoding/xml, which does not implement XML 1.0 §3.3.3 either, so they
// need the same treatment and there is no reason for a second copy of the rule.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// text is nil-safe: primary returns nil for an absent element and every caller
// wants "" for that.
func text(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return Collapse(e.Text())
}

// attr collapses too. Nothing upstream does it for us: XML 1.0 §3.3.3 would
// normalize a newline in an attribute value to a space, but encoding/xml, which
// etree wraps, does not implement it, so a tab or newline arrives verbatim.
func attr(e *etree.Element, name string) string {
	return Collapse(e.SelectAttrValue(name, ""))
}

// detach removes an element from wherever it sits: the parent may be a
// dc-metadata or x-metadata wrapper rather than <metadata>.
func detach(e *etree.Element) {
	if p := e.Parent(); p != nil {
		p.RemoveChild(e)
	}
}

func qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
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
