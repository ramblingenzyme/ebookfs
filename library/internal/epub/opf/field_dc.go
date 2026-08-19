package opf

import "github.com/beevik/etree"

// description and language are repeatable (§5.5.3.2.1) but single-valued to us,
// and have no encoding of their own, so they get no field type: target picks the
// element and writes go through setDCText.
func (o *Doc) description() string { return textOr(o.target("description")) }

func (o *Doc) language() string { return textOr(o.target("language")) }

func textOr(e *etree.Element) string {
	if e == nil {
		return ""
	}
	return text(e)
}
