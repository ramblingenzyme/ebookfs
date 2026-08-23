// Package xml holds the normalization rules that apply to every value read out
// of an epub's XML. They are types rather than helpers so that declaring a field
// is what applies the rule, leaving nothing to remember at each site.
package xml

import (
	"encoding/xml"
	"net/url"
	"strings"
)

// Collapse is the whitespace normalization XML 1.0 §3.3.3 requires of a reader.
// Neither encoding/xml nor etree applies it.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// AttrText is an attribute value, collapsed.
type AttrText string

func (a *AttrText) UnmarshalXMLAttr(x xml.Attr) error {
	*a = AttrText(Collapse(x.Value))
	return nil
}

// AttrURL is an attribute the spec types as a URL and we match against a zip
// entry name. Decoded is what a conforming file means; Raw is kept because a
// producer that wrote an unencoded name into both the XML and the zip has an
// entry whose name really does contain "%20".
type AttrURL struct{ Raw, Decoded string }

func (u *AttrURL) UnmarshalXMLAttr(x xml.Attr) error {
	u.Raw = Collapse(x.Value)
	u.Decoded = unescapePath(u.Raw)
	return nil
}

// Candidates returns the entry names this could mean, conforming form first.
func (u AttrURL) Candidates() []string {
	if u.Decoded == u.Raw {
		return []string{u.Raw}
	}
	return []string{u.Decoded, u.Raw}
}

// An invalid escape is not a path we can decode, so the literal stands — which
// is also what a name containing a bare '%' needs.
func unescapePath(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}
