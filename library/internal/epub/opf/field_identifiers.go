package opf

// identifiers keys by the element's XML id, which is the wrong key and frozen
// that way: changing it is a schema migration, and
// TestIdentifiersKeepTheirCurrentKeying guards it. Read-only.
func (o *Doc) identifiers() map[string]string {
	out := map[string]string{}
	for _, el := range o.elements("identifier") {
		out[attr(el, "id")] = text(el)
	}
	return out
}
