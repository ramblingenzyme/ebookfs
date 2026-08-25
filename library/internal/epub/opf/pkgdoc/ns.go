package pkgdoc

import "github.com/beevik/etree"

const (
	opfNamespace = "http://www.idpf.org/2007/opf"
	dcNamespace  = "http://purl.org/dc/elements/1.1/"
)

// ns resolves xmlns: prefixes against the package element's declarations. It is
// vocab's twin, for the other of the two naming systems: these prefixes are
// resolved by the XML parser, vocabulary prefixes live inside attribute values.
// Each has a get-or-declare step — prefix here, spell there.
type ns struct{ pkg *etree.Element }

// prefix returns the xmlns: prefix bound to uri, declaring preferred if the
// document binds none.
//
// Only <package>'s attributes are scanned, so a declaration further down (OPF
// 2.0 §2.2's example puts xmlns:opf on <metadata>) gets a redundant second one
// at the top. Harmless: both bind the same URI.
func (n ns) prefix(uri, preferred string) string {
	for i := range n.pkg.Attr {
		a := n.pkg.Attr[i]
		if a.Space == "xmlns" && a.Value == uri {
			return a.Key
		}
	}
	n.pkg.CreateAttr("xmlns:"+preferred, uri)
	return preferred
}

// opf returns the prefix bound to the OPF namespace. opf:role and opf:file-as
// need one because attributes cannot use a default namespace.
func (n ns) opf() string { return n.prefix(opfNamespace, "opf") }

// dcPrefix takes the Dublin Core xmlns: prefix from <dc:title>, so a new dc
// element matches the existing declaration. It sits on Doc rather than either
// half, being the one question that asks both.
func (d *Doc) dcPrefix() string {
	if els := d.md.children("title"); len(els) > 0 {
		return els[0].Space
	}
	// Nothing to copy, so use the declarations: an undeclared prefix would put
	// the new element in no namespace at all.
	return d.ns.prefix(dcNamespace, "dc")
}
