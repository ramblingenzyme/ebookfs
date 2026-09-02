package opf

import (
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf/pkgdoc"
)

// The identifier field: read-only, but with more encoding to describe than the
// one-liners in fields.go. EPUB names an identifier's scheme two ways, one per
// version, and when neither is present the value may name it itself. The order
// those are consulted in is the whole field.

// identifiers keys each dc:identifier by its scheme — isbn, uuid, doi — since
// that is what the value is, and what the index stores it under. The element's
// XML id is a document-local handle chosen by whoever produced the file and says
// nothing about the kind of identifier, so it is only the last resort — and an
// identifier the file names in no way at all still lands under a key of its own
// rather than being dropped. Read-only.
//
// Two identifiers can resolve to one scheme (ISBN-10 and ISBN-13 both being
// isbn), and neither the map nor the index's UNIQUE (book_id, scheme) can hold
// both: the first in document order wins.
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
//     name is still carried rather than dropped. Two of these can still collide
//     — ids differing only in case, or a malformed file repeating one — and they
//     collide the same way any other duplicate scheme does.
//
// An element carrying no id leaves even that unanswered, and identifiers gives
// it a numbered unknown key: having no name is not a reason to lose the value.
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

// onixIdentifierTypes maps the codes we can name to the schemes we key by, with
// the leading zero already stripped. Checked against list 5 of ONIX issue 74;
// it is a subset, so a code absent here is unrecognised rather than invalid.
// ISBN-10 (02) and ISBN-13 (15) are both isbn: the distinction is a property of
// the value, not of the kind of thing it is.
//
// An unrecognised code leaves the scheme underived on purpose, so the value's
// own URN or the element's id gets a turn. Three codes are left out deliberately
// rather than for want of a name: 01 (proprietary) and 22 (URN) both say the
// type is recorded elsewhere, and 24 is a co-publisher's ISBN-13 — another
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

// urnScheme is the one scheme name that answers nothing. A file spelling it —
// as an opf:scheme, or as the ONIX code 22 left out of the map above — says only
// that the kind is recorded in the value itself, which is where urnNID reads it.
// Both spellings are passed over so the two EPUB versions agree on what a URN
// identifier is keyed by.
const urnScheme = "urn"

// urnNID splits a URN into its namespace identifier and the rest. RFC 8141 §2.1
// makes NIDs case-insensitive ("ISBN" and "isbn" are equivalent) and §3.1 case-
// normalizes the "urn" token too, so both are matched without regard to case and
// the NID is lowercased to match the schemes we key by. The remainder is
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

// unknownScheme keys a dc:identifier the file names in no way at all: no
// opf:scheme, no identifier-type, no URN in the value, not even an XML id to
// borrow. The value is an identifier all the same, and dropping it would lose
// data, so it lands here.
const unknownScheme = "unknown"

// unusedUnknownScheme returns the first unknown key the map does not already
// hold — unknown, then unknown-2, unknown-3. Numbering rather than sharing one
// key is what keeps first-wins from eating the second unnamed identifier in a
// file: nothing about these values distinguishes them, so position is all there
// is to key by, and the keys are stable for a given file because the elements
// are read in document order.
//
// A file whose own scheme or id is literally "unknown" simply occupies the key
// and the numbering steps past it; one arriving after an unnamed identifier has
// taken it loses the way any other duplicate scheme does.
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
