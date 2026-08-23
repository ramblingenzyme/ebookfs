package epub

import (
	"encoding/xml"
	"net/url"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
)

// Attribute types for the container's XML files, carrying the two rules that
// apply to every value we read out of them. Declaring a field as one of these is
// what applies the rule; there is no way to read the attribute and forget.
//
// That is the point. Both rules were broken and fixed separately in container.xml
// and encryption.xml, because each was a thing to remember at each site. The opf
// package never had that problem: reading an attribute there *is* calling the
// accessor that normalizes it.
//
// These live in epub rather than opf because opf reads its document through
// etree and never touches encoding/xml, so nothing there can use them — and epub
// importing opf for Collapse is fine, while the reverse would be a cycle.

// attrText is an attribute value with the normalization XML 1.0 §3.3.3 requires.
// encoding/xml does not apply it, so a container that wraps or pads an attribute
// would otherwise fail every comparison against it.
type attrText string

func (a *attrText) UnmarshalXMLAttr(x xml.Attr) error {
	*a = attrText(opf.Collapse(x.Value))
	return nil
}

// attrURL is an attribute the spec types as a URL — a path-relative-scheme-less-URL
// in OCF's terms — which is matched against a zip entry name.
//
// Both forms are kept. Decoded is what a conforming file means: a space is
// written %20 and the entry holds the decoded name. Raw is the value as written,
// because a producer that wrote an unencoded name into both the XML and the zip
// has an entry whose name really does contain "%20", and only the raw form
// matches that.
type attrURL struct{ Raw, Decoded string }

func (u *attrURL) UnmarshalXMLAttr(x xml.Attr) error {
	u.Raw = opf.Collapse(x.Value)
	u.Decoded = unescapePath(u.Raw)
	return nil
}

// candidates returns the entry names this attribute could name, in the order to
// try them: what a conforming file means first, then what a careless one wrote.
func (u attrURL) candidates() []string {
	if u.Decoded == u.Raw {
		return []string{u.Raw}
	}
	return []string{u.Decoded, u.Raw}
}

// unescapePath decodes the percent-escapes in a URL-typed attribute. An invalid
// escape is not a path we can decode, so the literal stands — which is also what
// a name containing a bare '%' needs.
func unescapePath(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}
