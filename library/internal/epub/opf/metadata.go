package opf

import "github.com/beevik/etree"

// Finding metadata elements and creating them: which children of <metadata> a
// tag means, which of them a field acts on, where a new one belongs, and what
// prefix to give it. Reading and writing are kept side by side here so the two
// cannot drift — a write has to land on the element the matching read returned.
//
// The deprecated OPF 2.0 §2.2 wrappers are the reason most of this is not a
// one-liner. They are an EPUB 2 mechanism, but they decide where every element
// is found and created regardless of version, so they live here rather than
// with the version-specific encodings in the fields.

const opfNamespace = "http://www.idpf.org/2007/opf"

// elements returns the metadata children with the given tag, flattening the
// dc-metadata/x-metadata wrappers so no field has to know about them.
// dcParent and metaParent are the write half of the same rule.
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

// dcParent is where Dublin Core elements belong and metaParent where <meta>
// does: whichever deprecated wrapper the file already uses, or <metadata> when
// it uses neither. OPF 2.0 §2.2 makes the placement a MUST.
func (o *Doc) dcParent() *etree.Element {
	if w := o.md.SelectElement("dc-metadata"); w != nil {
		return w
	}
	return o.md
}

func (o *Doc) metaParent() *etree.Element {
	if w := o.md.SelectElement("x-metadata"); w != nil {
		return w
	}
	return o.md
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

// namedMetaElements finds the EPUB 2 <meta name="..."> pairs that predate
// refinements. namedMetaSlot is the write half.
func (o *Doc) namedMetaElements(name string) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if attr(m, "name") == name {
			out = append(out, m)
		}
	}
	return out
}

// dcPrefix takes the Dublin Core prefix from the always-present <dc:title>, so
// a new dc element matches the existing declaration. Defaults to "dc".
func (o *Doc) dcPrefix() string {
	if els := o.elements("title"); len(els) > 0 {
		return els[0].Space
	}
	return "dc"
}

// ensureOPFPrefix returns the prefix bound to the OPF namespace, declaring
// xmlns:opf if the document only has it as the default namespace. Attributes
// cannot use a default namespace, so opf:role and opf:file-as need a prefix.
func (o *Doc) ensureOPFPrefix() string {
	for i := range o.pkg.Attr {
		a := o.pkg.Attr[i]
		if a.Space == "xmlns" && a.Value == opfNamespace {
			return a.Key
		}
	}
	o.pkg.CreateAttr("xmlns:opf", opfNamespace)
	return "opf"
}
