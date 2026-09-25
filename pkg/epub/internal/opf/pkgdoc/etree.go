package pkgdoc

import (
	"strconv"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

func text(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return xml.Collapse(e.Text())
}

// attr collapses whitespace, since etree does not apply XML 1.0 §3.3.3.
func attr(e *etree.Element, name string) string {
	return xml.Collapse(e.SelectAttrValue(name, ""))
}

// detach removes an element from its parent, which may be a dc-metadata or
// x-metadata wrapper rather than <metadata>.
func detach(e *etree.Element) {
	if p := e.Parent(); p != nil {
		p.RemoveChild(e)
	}
}

// ensureID returns the element's id, minting stem, stem-2, … if it has none.
// It checks every id in the document, since XML 1.0 §3.3.1 makes ids unique
// document-wide.
//
// ponytail: rescans per call, O(n²) over a document holding tens of elements.
// Thread a set through the callers only if a profile ever says to.
func (d *Doc) ensureID(el *etree.Element, stem string) string {
	if id := attr(el, "id"); id != "" {
		return id
	}
	taken := map[string]bool{}
	for _, e := range d.pkg.FindElements("//*[@id]") {
		taken[attr(e, "id")] = true
	}
	id := stem
	for n := 2; taken[id]; n++ {
		id = stem + "-" + strconv.Itoa(n)
	}
	el.CreateAttr("id", id)
	return id
}
