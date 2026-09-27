package opf

import (
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// identifiers keys each dc:identifier by its scheme, such as isbn or doi,
// falling back to its XML id and then a numbered unknown key
// (docs/DECISIONS.md #24). Where two resolve to one scheme, as ISBN-10 and
// ISBN-13 do, the first in document order wins.
func (o *Doc) identifiers() map[string]string {
	out := map[string]string{}
	for _, el := range o.d.DCAll("identifier") {
		value := el.Get()
		if value == "" {
			continue
		}
		scheme := o.identifierScheme(el)
		if scheme == "" {
			scheme = unusedUnknownScheme(out)
		}
		if _, taken := out[scheme]; taken {
			continue
		}
		// A urn: prefix that repeats the scheme is dropped. Under another
		// scheme it carries information, so it stays.
		if nid, rest, ok := urnNID(value); ok && nid == scheme {
			value = rest
		}
		out[scheme] = value
	}
	return out
}

// identifierScheme names the kind of identifier an element carries, taking the
// first answer the file gives:
//
//   - the EPUB 2 opf:scheme attribute, unless it says urn, which only says the
//     kind is in the value;
//   - the EPUB 3 identifier-type refinement: an ONIX codelist 5 code when
//     schemed as one, or a plain name when unschemed. A code from any other
//     list is ignored;
//   - the URN namespace in the value itself, since urn:isbn: is as explicit as
//     a scheme saying isbn;
//   - the element's XML id. Two ids can collide, and they then collide like
//     any other duplicate scheme.
//
// It returns "" when none of these names the kind.
func (o *Doc) identifierScheme(el *pkgdoc.Element) string {
	if s := normalizeScheme(el.OPFAttr("scheme").Get()); s != "" && s != urnScheme {
		return s
	}

	kind := el.Refine("identifier-type")
	if code := kind.Schemed(onixCodelist5).Get(); code != "" {
		if name := onixIdentifierTypes[strings.TrimLeft(code, "0")]; name != "" {
			return name
		}
	}
	if s := kind.Unschemed().Get(); s != "" {
		return normalizeScheme(s)
	}

	if nid, _, ok := urnNID(el.Get()); ok {
		return nid
	}

	return normalizeScheme(el.ID())
}

// onixCodelist5 is ONIX's "Product identifier type" list, which D.3.8 gives as
// the example scheme for identifier-type.
const onixCodelist5 = "onix:codelist5"

// onixIdentifierTypes maps codes to scheme keys, with the leading zero
// stripped. Checked against list 5 of ONIX issue 74. It is a subset, so a code
// missing here is unrecognised, not invalid, and the value's URN or the id
// gets a turn. ISBN-10 (02) and ISBN-13 (15) are both isbn.
//
// Three codes are left out on purpose. 01 (proprietary) and 22 (URN) say the
// type is recorded elsewhere. 24 is a co-publisher's ISBN-13, another
// edition's identifier, which would be wrong under isbn.
var onixIdentifierTypes = map[string]string{
	"2":  "isbn",
	"3":  "gtin-13",
	"4":  "upc",
	"5":  "ismn",
	"6":  "doi",
	"13": "lccn",
	"14": "gtin-14",
	"15": "isbn",
	"17": "legal-deposit",
	"25": "ismn",
	"26": "isbn-a",
	"35": "ark",
}

// urnScheme, as an opf:scheme or ONIX code 22, says only that the kind is in
// the value. Both are passed over, so the two EPUB versions key a URN
// identifier alike.
const urnScheme = "urn"

// urnNID splits a URN into its namespace identifier and the rest. RFC 8141
// §2.1 makes the NID case-insensitive and §3.1 does the same for "urn", so
// both match any case and the NID is lowercased. The rest is returned as
// written.
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

// unusedUnknownScheme returns the first of unknown, unknown-2, unknown-3 that
// out does not hold, so first-wins cannot drop a second unnamed identifier.
func unusedUnknownScheme(out map[string]string) string {
	if _, taken := out[unknownScheme]; !taken {
		return unknownScheme
	}
	for n := 2; ; n++ {
		scheme := unknownScheme + "-" + strconv.Itoa(n)
		if _, taken := out[scheme]; !taken {
			return scheme
		}
	}
}

// normalizeScheme lowercases, since ISBN and isbn are one scheme.
func normalizeScheme(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
