package opf

import "time"

type modifiedField struct{ o *Doc }

func (o *Doc) modified() modifiedField { return modifiedField{o} }

// set records the time of this rewrite. EPUB 3.3 §5.5.5 requires the package to
// carry exactly one unrefined dcterms:modified, and §5.5.4 fixes the format:
// "CCYY-MM-DDThh:mm:ssZ", in UTC, terminated with Z. Updating it is a lowercase
// "should" and so not RFC 2119 binding, but a rewrite is exactly what makes the
// file new — leaving the old value is the writer claiming a change never
// happened.
//
// There is no get: nothing in ebookfs reads it. EPUB 2 has no equivalent, so
// nothing is written there either.
func (f modifiedField) set(t time.Time) {
	if !f.o.epub3() {
		return
	}
	value := t.UTC().Format("2006-01-02T15:04:05Z")
	for _, m := range f.o.elements("meta") {
		// Unrefined: a dcterms:modified with a refines attribute is the
		// modification date of whatever it refines, not of the package.
		if attr(m, "property") == "dcterms:modified" && attr(m, "refines") == "" {
			m.SetText(value)
			return
		}
	}
	m := f.o.metaHome().CreateElement("meta")
	m.CreateAttr("property", "dcterms:modified")
	m.SetText(value)
}
