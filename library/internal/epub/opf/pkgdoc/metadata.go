package pkgdoc

import "github.com/beevik/etree"

// metadata is the children of <metadata>: finding them and creating them.
// Reading and writing sit side by side so a write lands on the element the
// matching read returned.
//
// The deprecated OPF 2.0 §2.2 wrappers are why most of this is not a one-liner:
// they decide where every element is found and created regardless of version.
type metadata struct{ md *etree.Element }

// children returns the metadata children with the given tag, flattening the
// dc-metadata/x-metadata wrappers; dcParent and metaParent are the write half.
// Matching is on etree's Tag, the local name, so any dc prefix matches.
func (m metadata) children(tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range m.md.ChildElements() {
		switch c.Tag {
		case "dc-metadata", "x-metadata":
			for _, w := range c.ChildElements() {
				if w.Tag == tag {
					out = append(out, w)
				}
			}
		case tag:
			out = append(out, c)
		}
	}
	return out
}

// dcParent is where Dublin Core elements belong and metaParent where <meta>
// does: whichever deprecated wrapper the file already uses, or <metadata> when
// it uses neither. OPF 2.0 §2.2 makes the placement a MUST.
func (m metadata) dcParent() *etree.Element {
	if w := m.md.SelectElement("dc-metadata"); w != nil {
		return w
	}
	return m.md
}

// metaParent creates the x-metadata wrapper when the file uses dc-metadata
// without one — the common shape, since a producer with no non-DC metadata has
// no reason to emit it. §2.2: "all other metadata elements, if any, must go
// into x-metadata".
func (m metadata) metaParent() *etree.Element {
	if w := m.md.SelectElement("x-metadata"); w != nil {
		return w
	}
	if m.md.SelectElement("dc-metadata") == nil {
		return m.md
	}
	return m.md.CreateElement("x-metadata")
}

// primary is the element a read and a write of a field both mean: the first with
// a non-empty value (§5.5.3.1.2 for the title), or the first present if all are
// empty, since a write has to land somewhere. §5.5.2 requires non-empty values,
// so skipping an empty one recovers a book rather than losing it.
func (m metadata) primary(tag string) *etree.Element {
	els := m.children(tag)
	for _, e := range els {
		if text(e) != "" {
			return e
		}
	}
	if len(els) > 0 {
		return els[0]
	}
	return nil
}

// named finds the EPUB 2 <meta name="..."> pairs that predate refinements.
// Named is the write half.
func (m metadata) named(name string) []*etree.Element {
	var out []*etree.Element
	for _, e := range m.children("meta") {
		if attr(e, "name") == name {
			out = append(out, e)
		}
	}
	return out
}
