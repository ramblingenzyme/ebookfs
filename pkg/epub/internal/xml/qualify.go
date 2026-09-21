package xml

// Qualify joins an xmlns prefix to a local name, so an element created beside a
// sibling lands in the same namespace the file already uses. The empty prefix
// is the default namespace and needs no colon.
//
// An xmlns prefix only. A vocabulary prefix, the "dcterms:" in a property
// value, is a different thing that resolves through the package document's own
// prefix mappings.
func Qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}
