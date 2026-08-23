package opf

import (
	"maps"
	"slices"
	"strconv"
	"strings"
)

// Property names are names in a vocabulary, not the literal strings written in
// the attribute. A document binds its own prefixes with the package element's
// prefix attribute (D.1.4), so "dct:modified" under
// prefix="dct: http://purl.org/dc/terms/" is the same property as the reserved
// spelling "dcterms:modified".
//
// These are not XML namespaces, and nothing here is related to the xmlns:
// prefixes metadata.go deals in. A vocabulary prefix is bound by the package
// element's prefix attribute and appears only inside the *value* of property=
// and scheme=, where XML namespace processing does not reach — which is why no
// document declares one with xmlns:.
//
// Resolution is asymmetric:
//   - a name written in the document resolves through its declarations. D.1.4 is
//     how an author says what a prefix means here, and D.1.5's "SHOULD NOT
//     override reserved prefixes" admits circumstances where doing so is
//     legitimate, so a declaration is honoured even for a reserved name.
//   - a name this package spells resolves through the reserved table only.
//     "dcterms:modified" in our code means the DCMI term whatever the document
//     rebinds it to.
//
// Honouring the declaration is therefore also the defence: a document that
// rebinds dcterms has a dcterms:modified that is somebody else's property, the
// two sides no longer match, and we leave it alone instead of overwriting it
// with a timestamp.

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
	// D.1.4's grammar is a whitespace-separated list of "prefix: URL" pairs.
	// Fields handles the single-space and newline-wrapped spellings alike; a
	// trailing prefix with no URL is ignored rather than bound to nothing.
	fields := strings.Fields(attr(o.pkg, "prefix"))
	for i := 0; i+1 < len(fields); i += 2 {
		if name, ok := strings.CutSuffix(fields[i], ":"); ok && name != "" {
			m[name] = fields[i+1]
		}
	}
	return m
}

// expand resolves a name against one set of bindings. An unprefixed name is in
// the default vocabulary and an unbound prefix cannot be resolved; both come
// back unchanged, comparable to themselves and to nothing else.
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

// sameProperty reports whether a property name written in the document means the
// property this package calls ours. The argument order matters: the two sides
// resolve through different maps.
//
// There is deliberately no inDoc == ours fast path. Identical spellings are
// exactly the case the asymmetry exists for — a document that rebinds dcterms
// writes the same eight characters we do and means something else — so comparing
// literals first would short-circuit past the only check that can tell them
// apart. Every genuinely equal case still matches after expansion.
//
// It follows that a name this package spells must use a reserved prefix or the
// default vocabulary, since nothing else resolves against reservedPrefixes.
// Every one of ours is unprefixed but for dcterms:modified.
func (o *Doc) sameProperty(inDoc, ours string) bool {
	return expand(inDoc, o.vocabularies()) == expand(ours, reservedPrefixes)
}

// hasProperty reports whether a space-separated property list contains one.
// §5.9.1's properties attribute is "a space-separated list of property values",
// so membership is a token comparison: a substring test would match
// my-cover-image, which is a different property belonging to someone else. Each
// token is a name like any other, so it resolves the same way.
func (o *Doc) hasProperty(list, want string) bool {
	for _, token := range strings.Fields(list) {
		if o.sameProperty(token, want) {
			return true
		}
	}
	return false
}

// spell returns how to write one of our property names in this document. Almost
// always the name unchanged — but a document that rebound our prefix would make
// that name mean something else, so another prefix bound to the right vocabulary
// is used instead, declared if the document has none.
func (o *Doc) spell(ours string) string {
	prefix, local, ok := strings.Cut(ours, ":")
	if !ok {
		return ours // default vocabulary, nothing to rebind
	}
	url := reservedPrefixes[prefix]
	if url == "" || o.vocabularies()[prefix] == url {
		return ours
	}
	// Lowest name wins, so a document binding two prefixes to one vocabulary
	// gets the same spelling on every run rather than whichever map iteration
	// reached first.
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
