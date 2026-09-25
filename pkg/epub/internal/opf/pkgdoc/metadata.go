package pkgdoc

import "github.com/beevik/etree"

// metadata finds and creates the children of <metadata>. The deprecated OPF
// 2.0 §2.2 wrappers decide where every element is found and created, whatever
// the version.
type metadata struct{ md *etree.Element }

// children matches the local name, so any dc prefix matches.
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

// dcParent and metaParent return the wrapper the file uses, or <metadata> if
// it uses none. OPF 2.0 §2.2 makes the placement a MUST.
func (m metadata) dcParent() *etree.Element {
	if w := m.md.SelectElement("dc-metadata"); w != nil {
		return w
	}
	return m.md
}

// metaParent creates x-metadata when the file has only dc-metadata, which is
// common. §2.2: "all other metadata elements, if any, must go into
// x-metadata".
func (m metadata) metaParent() *etree.Element {
	if w := m.md.SelectElement("x-metadata"); w != nil {
		return w
	}
	if m.md.SelectElement("dc-metadata") == nil {
		return m.md
	}
	return m.md.CreateElement("x-metadata")
}

// primary is the first element with a non-empty value (§5.5.3.1.2 for the
// title), or the first if all are empty, since a write has to land somewhere.
// §5.5.2 requires non-empty values, so skipping an empty one recovers a book.
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

func (m metadata) named(name string) []*etree.Element {
	var out []*etree.Element
	for _, e := range m.children("meta") {
		if attr(e, "name") == name {
			out = append(out, e)
		}
	}
	return out
}
