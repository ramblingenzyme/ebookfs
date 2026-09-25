package pkgdoc

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/beevik/etree"
)

// vocab resolves property names against the prefixes <package> binds (D.1.4).
// They live inside property= and scheme= values, where XML namespaces do not
// reach.
//
// Resolution is asymmetric. A name in the document resolves through its
// declarations, even a rebound reserved prefix, which D.1.5 permits. A name
// this package writes resolves through the reserved table only. So in a
// document that rebinds dcterms, its dcterms:modified is someone else's and is
// left alone.
type vocab struct{ pkg *etree.Element }

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

func (v vocab) bindings() map[string]string {
	m := make(map[string]string, len(reservedPrefixes))
	maps.Copy(m, reservedPrefixes)
	// D.1.4: a whitespace-separated list of "prefix: URL" pairs.
	fields := strings.Fields(attr(v.pkg, "prefix"))
	for i := 0; i+1 < len(fields); i += 2 {
		if name, ok := strings.CutSuffix(fields[i], ":"); ok && name != "" {
			m[name] = fields[i+1]
		}
	}
	return m
}

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

// Same reports whether inDoc, as the document spells it, names the same
// property as ours. The two sides resolve differently, so order matters.
//
// There is no inDoc == ours shortcut, since identical spellings are the case
// the asymmetry exists for.
func (v vocab) Same(inDoc, ours string) bool {
	return expand(inDoc, v.bindings()) == expand(ours, reservedPrefixes)
}

// has reports whether a properties list contains want. §5.9.1 makes it "a
// space-separated list of property values", so a substring test would match
// my-cover-image.
func (v vocab) has(list, want string) bool {
	for token := range strings.FieldsSeq(list) {
		if v.Same(token, want) {
			return true
		}
	}
	return false
}

// spell returns how to write one of this package's names in this document.
// If the document rebound the prefix, it uses another prefix bound to the
// right vocabulary, declaring one if needed.
func (v vocab) spell(ours string) string {
	prefix, local, ok := strings.Cut(ours, ":")
	if !ok {
		return ours // default vocabulary, nothing to rebind
	}
	url := reservedPrefixes[prefix]
	if url == "" || v.bindings()[prefix] == url {
		return ours
	}
	// Lowest name wins, so the spelling is stable across runs.
	candidates := make([]string, 0, 1)
	for name, bound := range v.bindings() {
		if bound == url {
			candidates = append(candidates, name)
		}
	}
	if len(candidates) > 0 {
		return slices.Min(candidates) + ":" + local
	}
	return v.declare(prefix, url) + ":" + local
}

func (v vocab) declare(preferred, url string) string {
	name := preferred
	for i := 2; ; i++ {
		if _, taken := v.bindings()[name]; !taken {
			break
		}
		name = preferred + strconv.Itoa(i)
	}

	decl := name + ": " + url
	if existing := attr(v.pkg, "prefix"); existing != "" {
		decl = existing + " " + decl
	}
	v.pkg.CreateAttr("prefix", decl)
	return name
}
