package opf

import "time"

type modifiedField struct{ o *Doc }

func (o *Doc) modified() modifiedField { return modifiedField{o} }

// set records the time of this rewrite: exactly one unrefined dcterms:modified
// per §5.5.5, in the UTC format §5.5.4 fixes. Write-only, and EPUB 3 only.
func (f modifiedField) set(t time.Time) {
	if !f.o.epub3() {
		return
	}
	value := t.UTC().Format("2006-01-02T15:04:05Z")
	for _, m := range f.o.elements("meta") {
		// A refining dcterms:modified dates what it refines, not the package.
		if attr(m, "property") == "dcterms:modified" && attr(m, "refines") == "" {
			m.SetText(value)
			return
		}
	}
	m := f.o.metaHome().CreateElement("meta")
	m.CreateAttr("property", "dcterms:modified")
	m.SetText(value)
}
