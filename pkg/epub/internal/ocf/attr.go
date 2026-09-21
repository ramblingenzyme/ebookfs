package ocf

import (
	"encoding/xml"
	"net/url"

	epubxml "github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// The attribute types the container's documents declare their fields with.
// Types rather than helpers, so declaring a field is what applies the rule and
// nothing has to be remembered at each read.
//
// They live here rather than in internal/xml because they only work through
// encoding/xml, and this is the only package that uses it: every other document
// in an epub is edited as well as read, which is etree's job.

// AttrText is an attribute value, collapsed.
type AttrText string

func (a *AttrText) UnmarshalXMLAttr(x xml.Attr) error {
	*a = AttrText(epubxml.Collapse(x.Value))
	return nil
}

// AttrURL is an attribute the spec types as a URL and this package matches
// against a zip entry name. Decoded is what a conforming file means. Raw is
// kept because a producer that wrote an unencoded name into both the XML and
// the zip has an entry whose name really does contain "%20".
type AttrURL struct{ Raw, Decoded string }

func (u *AttrURL) UnmarshalXMLAttr(x xml.Attr) error {
	u.Raw = epubxml.Collapse(x.Value)
	u.Decoded = unescapePath(u.Raw)
	return nil
}

// Candidates returns the entry names this could mean, conforming form first.
func (u *AttrURL) Candidates() []string {
	if u.Decoded == u.Raw {
		return []string{u.Raw}
	}
	return []string{u.Decoded, u.Raw}
}

// An invalid escape is not a decodable path, so the literal stands, which
// is also what a name containing a bare '%' needs.
func unescapePath(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}
