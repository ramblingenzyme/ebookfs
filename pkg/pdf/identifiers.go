package pdf

import (
	"encoding/xml"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xmp"
)

// identifiers keys the packet's identifiers by scheme, as pkg/epub does
// (docs/DECISIONS.md #24), from the places writers put them, in this order:
//
//   - xmp:Identifier, whose items each name their scheme in an xmpidq:Scheme
//     qualifier, as calibre writes them;
//   - dc:identifier;
//   - PRISM's doi and isbn, as publishers write them.
//
// A value with no stated scheme takes its own urn: namespace, failing that a
// numbered unknown key. Where two resolve to one scheme, the first wins.
func identifiers(p *xmp.Packet) map[string]string {
	out := idSet{}

	items, _ := p.Items(xmpNS("Identifier"))
	for _, it := range items {
		out.add(normalizeScheme(it.Qualifiers[xml.Name{Space: xmp.NSXMPIdq, Local: "Scheme"}]), it.Value)
	}

	values, _ := p.Values(dc("identifier"))
	for _, v := range values {
		out.add("", v)
	}

	for _, scheme := range []string{"doi", "isbn"} {
		for _, ns := range xmp.NSPRISM {
			if v, _ := p.Values(xml.Name{Space: ns, Local: scheme}); len(v) > 0 {
				out.add(scheme, v[0])
			}
		}
	}

	return out
}

type idSet map[string]string

// add keys value by scheme, or by its urn: namespace when scheme is "", or
// else by the next unknown key. A scheme already taken keeps its value.
func (out idSet) add(scheme, value string) {
	if value == "" {
		return
	}

	if scheme == "" {
		scheme, _, _ = urnNID(value)
	}

	if scheme == "" {
		scheme = unusedUnknownScheme(out)
	}

	if _, taken := out[scheme]; taken {
		return
	}

	// A urn: prefix that repeats the scheme is dropped. Under another scheme
	// it carries information, so it stays.
	if nid, rest, ok := urnNID(value); ok && nid == scheme {
		value = rest
	}

	out[scheme] = value
}

// urnNID splits a urn: value into its lowercased namespace and the rest.
func urnNID(value string) (nid, rest string, ok bool) {
	const prefix = "urn:"
	if len(value) < len(prefix) || !strings.EqualFold(value[:len(prefix)], prefix) {
		return "", "", false
	}

	nid, rest, ok = strings.Cut(value[len(prefix):], ":")
	if !ok || nid == "" || rest == "" {
		return "", "", false
	}

	return strings.ToLower(nid), rest, true
}

const unknownScheme = "unknown"

// unusedUnknownScheme numbers unschemed identifiers, so a second is not lost
// to the first.
func unusedUnknownScheme(out idSet) string {
	scheme := unknownScheme
	for n := 2; ; n++ {
		if _, taken := out[scheme]; !taken {
			return scheme
		}

		scheme = unknownScheme + "-" + strconv.Itoa(n)
	}
}

// normalizeScheme lowercases, since ISBN and isbn are one scheme.
func normalizeScheme(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
