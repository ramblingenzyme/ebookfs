package opf

import "github.com/beevik/etree"

// Everything here exists only because EPUB 2 does; a field's own EPUB 2
// encoding lives with the field.

const opfNamespace = "http://www.idpf.org/2007/opf"

func (o *Doc) namedMetas(name string) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if attr(m, "name") == name {
			out = append(out, m)
		}
	}
	return out
}

func (o *Doc) namedMeta(name string) string {
	if ms := o.namedMetas(name); len(ms) > 0 {
		return attr(ms[0], "content")
	}
	return ""
}

// setNamedMeta updates the meta already there rather than replacing it, so it
// keeps its position in the document.
func (o *Doc) setNamedMeta(name, content string) {
	if ms := o.namedMetas(name); len(ms) > 0 {
		ms[0].CreateAttr("content", content)
		return
	}
	m := o.metaHome().CreateElement("meta")
	m.CreateAttr("name", name)
	m.CreateAttr("content", content)
}

func (o *Doc) removeNamedMeta(name string) {
	for _, m := range o.namedMetas(name) {
		detach(m)
	}
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

// dcHome is where Dublin Core elements belong and metaHome where <meta> does:
// whichever deprecated wrapper the file already uses, or <metadata> when it uses
// neither. OPF 2.0 §2.2 makes the placement a MUST.
func (o *Doc) dcHome() *etree.Element { return o.home("dc-metadata") }

func (o *Doc) metaHome() *etree.Element { return o.home("x-metadata") }

func (o *Doc) home(wrapper string) *etree.Element {
	if w := o.md.SelectElement(wrapper); w != nil {
		return w
	}
	return o.md
}
