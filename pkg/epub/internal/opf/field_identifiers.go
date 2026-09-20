package opf

import (
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
)

// identifiers keys each dc:identifier by its scheme (isbn, uuid, doi), which is
// what a caller filing it wants. The element's XML id is a document-local handle
// chosen by whoever produced the file and says nothing about the kind of
// identifier, so it is the last resort. An identifier the file names in no way
// at all still lands under a key of its own. Read-only.
//
// Two identifiers can resolve to one scheme (ISBN-10 and ISBN-13 both being
// isbn), and a map keyed by scheme cannot hold both: the first in document
// order wins.
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
		// A value that spells its own namespace under the scheme that namespace
		// names says the same thing twice; the bare identifier is what a lookup
		// or a comparison wants. A urn: under some other scheme is left whole,
		// since there the prefix is carrying information the key is not.
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
//   - the EPUB 2 opf:scheme attribute, which is where a v2 file says it, unless
//     it says urn: that names no kind, only where the kind is written, so the
//     value gets the turn the attribute declined;
//   - the EPUB 3 identifier-type refinement, whose value is an ONIX codelist 5
//     code when it is schemed as one and a plain name when it is unschemed. A
//     code from any other list is somebody else's and is passed over, the same
//     reading the series field applies to collection-type;
//   - the URN namespace the value itself declares, since urn:isbn: is as
//     explicit as an attribute saying isbn;
//   - failing all of those, the element's XML id, so an identifier nothing can
//     name is still carried. Two of these can collide, where ids differ only in
//     case or a malformed file repeats one, and they collide the same way any
//     other duplicate scheme does.
//
// An element carrying no id gets a numbered unknown key instead.
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

// onixCodelist5 is ONIX's "Product identifier type" list, the one D.3.8 gives as
// the example scheme for an identifier-type code.
const onixCodelist5 = "onix:codelist5"

// onixIdentifierTypes maps the nameable codes to the schemes used as keys, with
// the leading zero already stripped. Checked against list 5 of ONIX issue 74;
// it is a subset, so a code absent here is unrecognised rather than invalid.
// ISBN-10 (02) and ISBN-13 (15) are both isbn: the distinction is a property of
// the value, not of the kind of thing it is.
//
// An unrecognised code leaves the scheme underived on purpose, so the value's
// own URN or the element's id gets a turn. Three codes are left out deliberately
// rather than for want of a name: 01 (proprietary) and 22 (URN) both say the
// type is recorded elsewhere, and 24 is a co-publisher's ISBN-13, another
// edition's identifier, which would be wrong under isbn and worse under a key
// that implied it was this book's.
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

// urnScheme is the one scheme name that answers nothing. A file spelling it, as
// an opf:scheme or as the ONIX code 22 left out of the map above, says only
// that the kind is recorded in the value itself, which is where urnNID reads it.
// Both spellings are passed over so the two EPUB versions agree on what a URN
// identifier is keyed by.
const urnScheme = "urn"

// urnNID splits a URN into its namespace identifier and the rest. RFC 8141 §2.1
// makes NIDs case-insensitive ("ISBN" and "isbn" are equivalent) and §3.1 case-
// normalizes the "urn" token too, so both are matched without regard to case and
// the NID is lowercased to match the schemes used as keys. The remainder is
// returned as written, being the identifier itself.
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

// unknownScheme keys a dc:identifier with no opf:scheme, no identifier-type, no
// URN in the value and no XML id to borrow.
const unknownScheme = "unknown"

// unusedUnknownScheme returns the first unknown key the map does not hold:
// unknown, then unknown-2, unknown-3. Numbering keeps first-wins from eating the
// second unnamed identifier in a file. Nothing distinguishes these values, so
// document order is all there is to key by, which also makes the keys stable for
// a given file.
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

// normalizeScheme lowercases, since ISBN and isbn are one scheme and the key has
// to be stable across the files that spell it either way.
func normalizeScheme(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
