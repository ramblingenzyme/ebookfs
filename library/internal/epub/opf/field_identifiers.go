package opf

// identifiers returns the dc:identifier values keyed by the element's XML id.
//
// Keying by id is wrong: an id is a document-internal name its author chose
// ("pub-id", "isbn", "x1"), not the identifier's scheme, and the index persists
// it as one, so no caller can ask what a book's ISBN is.
//
// Left alone deliberately. Changing the keying changes what every reindex
// writes into identifiers.scheme, which is NOT NULL with UNIQUE (book_id,
// scheme) — every existing library already holds rows keyed this way, so this
// is a schema migration, not a parser fix, and must not happen as a side
// effect. TestIdentifiersKeepTheirCurrentKeying guards that.
//
// Read-only: nothing in ebookfs writes identifiers.
func (o *Doc) identifiers() map[string]string {
	out := map[string]string{}
	for _, el := range o.elements("identifier") {
		out[attr(el, "id")] = text(el)
	}
	return out
}
