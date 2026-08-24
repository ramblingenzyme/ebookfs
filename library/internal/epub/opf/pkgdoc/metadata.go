package pkgdoc

import "github.com/beevik/etree"

// Finding metadata elements and creating them. Reading and writing sit side by
// side so a write lands on the element the matching read returned.
//
// The deprecated OPF 2.0 §2.2 wrappers are why most of this is not a one-liner:
// they decide where every element is found and created regardless of version.

const (
	opfNamespace = "http://www.idpf.org/2007/opf"
	dcNamespace  = "http://purl.org/dc/elements/1.1/"
)

// elements returns the metadata children with the given tag, flattening the
// dc-metadata/x-metadata wrappers; dcParent and metaParent are the write half.
// Matching is on etree's Tag, the local name, so any dc prefix matches.
func (d *Doc) elements(tag string) []*etree.Element {
	var out []*etree.Element
	for _, c := range d.md.ChildElements() {
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
func (d *Doc) dcParent() *etree.Element {
	if w := d.md.SelectElement("dc-metadata"); w != nil {
		return w
	}
	return d.md
}

// metaParent creates the x-metadata wrapper when the file uses dc-metadata
// without one — the common shape, since a producer with no non-DC metadata has
// no reason to emit it. §2.2: "all other metadata elements, if any, must go
// into x-metadata".
func (d *Doc) metaParent() *etree.Element {
	if w := d.md.SelectElement("x-metadata"); w != nil {
		return w
	}
	if d.md.SelectElement("dc-metadata") == nil {
		return d.md
	}
	return d.md.CreateElement("x-metadata")
}

// primary is the element a read and a write of a field both mean: the first with
// a non-empty value (§5.5.3.1.2 for the title), or the first present if all are
// empty, since a write has to land somewhere. §5.5.2 requires non-empty values,
// so skipping an empty one recovers a book rather than losing it.
func (d *Doc) primary(tag string) *etree.Element {
	els := d.elements(tag)
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
// refinements. Named is the write half.
func (d *Doc) namedMetaElements(name string) []*etree.Element {
	var out []*etree.Element
	for _, m := range d.elements("meta") {
		if attr(m, "name") == name {
			out = append(out, m)
		}
	}
	return out
}

// dcPrefix takes the Dublin Core xmlns: prefix from <dc:title>, so a new dc
// element matches the existing declaration.
func (d *Doc) dcPrefix() string {
	if els := d.elements("title"); len(els) > 0 {
		return els[0].Space
	}
	// Nothing to copy, so use the declarations: an undeclared prefix would put
	// the new element in no namespace at all.
	return d.ensureNSPrefix(dcNamespace, "dc")
}

// ensureOPFPrefix returns the xmlns: prefix bound to the OPF namespace. opf:role
// and opf:file-as need one because attributes cannot use a default namespace.
func (d *Doc) ensureOPFPrefix() string { return d.ensureNSPrefix(opfNamespace, "opf") }

// ensureNSPrefix returns the xmlns: prefix bound to ns, declaring preferred if
// the document binds none. vocab.go's spell is the vocabulary-side twin.
//
// Only <package>'s attributes are scanned, so a declaration further down (OPF
// 2.0 §2.2's example puts xmlns:opf on <metadata>) gets a redundant second one
// at the top. Harmless: both bind the same URI.
func (d *Doc) ensureNSPrefix(ns, preferred string) string {
	for i := range d.pkg.Attr {
		a := d.pkg.Attr[i]
		if a.Space == "xmlns" && a.Value == ns {
			return a.Key
		}
	}
	d.pkg.CreateAttr("xmlns:"+preferred, ns)
	return preferred
}
