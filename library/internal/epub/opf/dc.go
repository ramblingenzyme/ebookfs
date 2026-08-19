package opf

import "github.com/beevik/etree"

// Dublin Core metadata elements — <dc:title>, <dc:creator> and the rest —
// selected for reading and for writing. Which element a write lands on is
// primary's business, so a read and a write can never disagree.

// elements returns the metadata children with the given tag, flattening the
// deprecated OPF 2.0 §2.2 dc-metadata/x-metadata wrappers so no field has to
// know about them. The write side is dcParent/metaParent in epub2.go.
//
// Matching is on etree's Tag, the local name with the prefix held separately,
// so it matches whatever prefix the document binds Dublin Core to.
func (o *Doc) elements(tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range o.md.ChildElements() {
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

// primary is the element a read and a write of a field both mean: the first
// with a non-empty value (§5.5.3.1.2 for the title), or the first present when
// they are all empty, since a write has to land somewhere.
//
// Empty values are skipped because §5.5.2 requires non-empty ones: an empty
// element is a malformed file, and the usable value after it beats no book.
func (o *Doc) primary(tag string) *etree.Element {
	els := o.elements(tag)
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

// dcPrefix takes the Dublin Core prefix from the always-present <dc:title>, so
// a new dc element matches the existing declaration. Defaults to "dc".
func (o *Doc) dcPrefix() string {
	if els := o.elements("title"); len(els) > 0 {
		return els[0].Space
	}
	return "dc"
}
