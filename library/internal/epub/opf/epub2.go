package opf

import "github.com/beevik/etree"

// Everything here exists only because EPUB 2 does; a field's own EPUB 2
// encoding lives with the field.

const opfNamespace = "http://www.idpf.org/2007/opf"

func (o *Doc) namedMetaElements(name string) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if attr(m, "name") == name {
			out = append(out, m)
		}
	}
	return out
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
