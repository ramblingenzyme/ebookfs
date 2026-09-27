package pkgdoc

import "github.com/beevik/etree"

const (
	opfNamespace = "http://www.idpf.org/2007/opf"
	dcNamespace  = "http://purl.org/dc/elements/1.1/"
)

// ns resolves xmlns: prefixes against <package>'s declarations. vocab is its
// counterpart for vocabulary prefixes, which live inside attribute values
// where the XML parser does not resolve them.
type ns struct{ pkg *etree.Element }

// Only <package> is scanned, so a declaration on <metadata> (OPF 2.0 §2.2)
// gets a harmless second one at the top.
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

// opf returns the OPF namespace prefix, which opf:role and opf:file-as need
// because attributes cannot use a default namespace.
func (n ns) opf() string { return n.prefix(opfNamespace, "opf") }

func (d *Doc) dcPrefix() string {
	if els := d.md.children("title"); len(els) > 0 {
		return els[0].Space
	}
	// An undeclared prefix would put the new element in no namespace.
	return d.ns.prefix(dcNamespace, "dc")
}
