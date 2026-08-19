package opf

import "github.com/beevik/etree"

// Everything here exists only because EPUB 2 does. Delete this file and the
// package speaks EPUB 3 only: <meta name= content=> pairs stop being read or
// written, opf: attributes need no prefix, and metadata children are simply the
// children of <metadata>.
//
// What is *not* here is any field's EPUB 2 encoding — the calibre:series metas,
// opf:role and opf:file-as on a creator, opf:event on a date, the legacy cover
// meta. Those live with their field, because a field owning the whole of its
// encoding in one place is what stopped the reader and the writer disagreeing
// about it. Splitting them by spec version would put the two halves of one rule
// back in two files.

const opfNamespace = "http://www.idpf.org/2007/opf"

// namedMetas returns every EPUB 2 <meta name= content=> with the given name.
func (o *Doc) namedMetas(name string) []*etree.Element {
	var out []*etree.Element
	for _, m := range o.elements("meta") {
		if attr(m, "name") == name {
			out = append(out, m)
		}
	}
	return out
}

// namedMeta returns the content of the first EPUB 2 meta with the given name.
func (o *Doc) namedMeta(name string) string {
	if ms := o.namedMetas(name); len(ms) > 0 {
		return attr(ms[0], "content")
	}
	return ""
}

// setNamedMeta writes an EPUB 2 <meta name= content=>, in place where one
// exists. CreateAttr replaces the attribute rather than appending, so the
// element keeps its position.
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
// xmlns:opf if the document only has it as the default namespace (attributes
// cannot use a default namespace, so prefixed opf:role/opf:file-as need one).
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

// dcHome is where Dublin Core elements belong, and metaHome where <meta> does.
// OPF 2.0 §2.2: "If the dc-metadata element is used, all dc elements must go in
// dc-metadata and all other metadata elements, if any, must go into
// x-metadata." Writing into the wrapper a file already uses keeps that true;
// a file using neither gets everything under <metadata>, as EPUB 3 requires.
func (o *Doc) dcHome() *etree.Element { return o.home("dc-metadata") }

func (o *Doc) metaHome() *etree.Element { return o.home("x-metadata") }

func (o *Doc) home(wrapper string) *etree.Element {
	if w := o.md.SelectElement(wrapper); w != nil {
		return w
	}
	return o.md
}
