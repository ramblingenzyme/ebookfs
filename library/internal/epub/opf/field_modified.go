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
	f.o.propertyMeta("dcterms:modified").set(t.UTC().Format("2006-01-02T15:04:05Z"))
}
