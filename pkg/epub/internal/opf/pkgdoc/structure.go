package pkgdoc

import "strings"

// The parts of the package document that hold no metadata: the manifest, and
// the two pointers into it. Reported as the document spells them — which item
// is the NCX, and which reference means the cover, are opf's rules.

type Item struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

// Ref is a <guide> reference, the OPF 2.0 §2.6 pointer kept by §5.9.4.
type Ref struct{ Type, Href string }

func (d *Doc) Manifest() []Item {
	m := d.pkg.SelectElement("manifest")
	if m == nil {
		return nil
	}
	var out []Item
	for _, it := range m.SelectElements("item") {
		out = append(out, Item{
			ID: attr(it, "id"),
			// Only trimmed, not collapsed: href is a percent-encoded URL, and
			// collapsing could rewrite a literal filename.
			Href:       strings.TrimSpace(it.SelectAttrValue("href", "")),
			MediaType:  attr(it, "media-type"),
			Properties: attr(it, "properties"),
		})
	}
	return out
}

func (d *Doc) Guide() []Ref {
	g := d.pkg.SelectElement("guide")
	if g == nil {
		return nil
	}
	var out []Ref
	for _, r := range g.SelectElements("reference") {
		out = append(out, Ref{
			Type: attr(r, "type"),
			Href: strings.TrimSpace(r.SelectAttrValue("href", "")),
		})
	}
	return out
}

// SpineFirst is the idref of the first document in the reading order, or "".
func (d *Doc) SpineFirst() string {
	spine := d.pkg.SelectElement("spine")
	if spine == nil {
		return ""
	}
	first := spine.SelectElement("itemref")
	if first == nil {
		return ""
	}
	return attr(first, "idref")
}
