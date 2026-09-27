package opf

import (
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

type subjectsField struct{ d *pkgdoc.Doc }

func (o *Doc) subjects() subjectsField { return subjectsField{o.d} }

// get returns all non-empty dc:subject elements in document order.
func (f subjectsField) get() []string {
	out := []string{}
	for _, el := range f.d.DCAll("subject") {
		if v := el.Get(); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// set reconciles subjects by text: elements with matching values are reused
// (preserving their authority/term refinements), unmatched elements are removed,
// and new values get new elements.
func (f subjectsField) set(subjects []string) {
	// Keyed by the text get reports, so a match is the element the caller was shown.
	byText := map[string]*pkgdoc.Element{}
	unclaimed := map[*pkgdoc.Element]bool{}
	for _, el := range f.d.DCAll("subject") {
		if text := el.Get(); text != "" {
			if _, seen := byText[text]; !seen {
				byText[text] = el
			}
		}
		unclaimed[el] = true
	}

	for _, text := range subjects {
		if el, reused := byText[text]; reused {
			delete(unclaimed, el)
			continue
		}
		el := f.d.NewDC("subject")
		el.Set(text)
		el.Place()
	}

	for el := range unclaimed {
		el.Remove()
	}
}

