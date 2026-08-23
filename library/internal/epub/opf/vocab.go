package opf

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Property names are names in a vocabulary, bound by the package element's
// prefix attribute (D.1.4). Not XML namespaces: they appear only inside the
// value of property= and scheme=, where namespace processing does not reach.
//
// Resolution is asymmetric. A name written in the document resolves through its
// declarations, honouring even a rebound reserved prefix (D.1.5 permits it); a
// name this package spells resolves through the reserved table only. So a
// document that rebinds dcterms has a dcterms:modified that is somebody else's
// property, the two sides do not match, and we leave it alone.

// reservedPrefixes are the D.1.5 prefixes, which creators "MAY use ... without
// having to declare them".
var reservedPrefixes = map[string]string{
	"a11y":      "http://www.idpf.org/epub/vocab/package/a11y/#",
	"dcterms":   "http://purl.org/dc/terms/",
	"marc":      "http://id.loc.gov/vocabulary/",
	"media":     "http://www.idpf.org/epub/vocab/overlays/#",
	"onix":      "http://www.editeur.org/ONIX/book/codelists/current.html#",
	"rendition": "http://www.idpf.org/vocab/rendition/#",
	"schema":    "http://schema.org/",
	"xsd":       "http://www.w3.org/2001/XMLSchema#",
}

// prefixes returns the document's prefix mappings: the reserved set overlaid
// with whatever the package element declares.
func (o *Doc) vocabularies() map[string]string {
	m := make(map[string]string, len(reservedPrefixes))
	maps.Copy(m, reservedPrefixes)
	// D.1.4: a whitespace-separated list of "prefix: URL" pairs.
	fields := strings.Fields(attr(o.pkg, "prefix"))
	for i := 0; i+1 < len(fields); i += 2 {
		if name, ok := strings.CutSuffix(fields[i], ":"); ok && name != "" {
			m[name] = fields[i+1]
		}
	}
	return m
}

// expand resolves a name against one set of bindings. Unprefixed or unbound
// names come back unchanged, comparable to themselves and nothing else.
func expand(name string, in map[string]string) string {
	prefix, local, ok := strings.Cut(name, ":")
	if !ok {
		return name
	}
	url, bound := in[prefix]
	if !bound {
		return name
	}
	return url + local
}

// sameProperty reports whether a name written in the document means the property
// we call ours. Argument order matters: the two sides resolve differently.
//
// No inDoc == ours fast path, deliberately: identical spellings are the case the
// asymmetry exists for. It follows that a name we spell must use a reserved
// prefix or the default vocabulary; all of ours do.
func (o *Doc) sameProperty(inDoc, ours string) bool {
	return expand(inDoc, o.vocabularies()) == expand(ours, reservedPrefixes)
}

// hasProperty reports whether a property list contains one. §5.9.1 makes it "a
// space-separated list of property values", so membership is a token comparison:
// a substring test would match my-cover-image, someone else's property.
func (o *Doc) hasProperty(list, want string) bool {
	for token := range strings.FieldsSeq(list) {
		if o.sameProperty(token, want) {
			return true
		}
	}
	return false
}

// spell returns how to write one of our property names in this document: the
// name unchanged, unless the document rebound our prefix, in which case another
// prefix bound to the right vocabulary is used and declared if there is none.
func (o *Doc) spell(ours string) string {
	prefix, local, ok := strings.Cut(ours, ":")
	if !ok {
		return ours // default vocabulary, nothing to rebind
	}
	url := reservedPrefixes[prefix]
	if url == "" || o.vocabularies()[prefix] == url {
		return ours
	}
	// Lowest name wins, so the spelling is stable across runs.
	candidates := make([]string, 0, 1)
	for name, bound := range o.vocabularies() {
		if bound == url {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) > 0 {
		return slices.Min(candidates) + ":" + local
	}
	return o.declarePrefix(prefix, url) + ":" + local
}

// declarePrefix adds a binding to the package element's prefix attribute and
// returns the prefix it bound, suffixing the preferred name until it is free.
func (o *Doc) declarePrefix(preferred, url string) string {
	name := preferred
	for i := 2; ; i++ {
		if _, taken := o.vocabularies()[name]; !taken {
			break
		}
		name = preferred + strconv.Itoa(i)
	}

	decl := name + ": " + url
	if existing := attr(o.pkg, "prefix"); existing != "" {
		decl = existing + " " + decl
	}
	o.pkg.CreateAttr("prefix", decl)
	return name
}
