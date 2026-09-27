package ocf

import (
	"encoding/xml"
	"net/url"

	epubxml "github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// AttrText is an attribute value with its whitespace collapsed. It and AttrURL
// live here rather than in internal/xml because only this package uses
// encoding/xml; the others edit their documents with etree.
type AttrText string

func (a *AttrText) UnmarshalXMLAttr(x xml.Attr) error {
	*a = AttrText(epubxml.Collapse(x.Value))
	return nil
}

// AttrURL is a URL attribute matched against zip entry names. Decoded is what a
// conforming file means. Raw is kept because some producers write the
// unencoded name into both the XML and the zip, so the entry's name really
// contains "%20".
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

// An invalid escape, such as a bare '%', leaves the name as written.
func unescapePath(s string) string {
	decoded, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return decoded
}
