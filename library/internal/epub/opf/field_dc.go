package opf

import "github.com/beevik/etree"

// description and language are Dublin Core elements holding a single value.
// Both are repeatable per §5.5.3.2.1, and target resolves a read and a write to
// the same one. Writes go through setDCText directly — there is no encoding
// here to own, unlike the other fields.
func (o *Doc) description() string { return textOr(o.target("description")) }

func (o *Doc) language() string { return textOr(o.target("language")) }

// textOr is text for an element that may be absent.
func textOr(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return text(e)
}
