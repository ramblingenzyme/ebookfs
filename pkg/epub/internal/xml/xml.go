// Package xml holds the XML rules more than one epub document format needs. A
// rule only one format needs lives with that format.
package xml

import (
	"net/url"
	"path"
	"strings"
)

// Collapse is the whitespace normalization XML 1.0 §3.3.3 requires of a reader.
// Neither encoding/xml nor etree applies it.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Qualify takes an xmlns prefix, not a vocabulary prefix such as the "dcterms:"
// in a property value.
func Qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}

// ResolveHref returns the container path an href in a document at baseDir
// names, dropping any fragment.
//
// It returns only the decoded form, which is what a conforming file means. A
// caller looking the result up as a zip entry misses a producer's literally
// encoded name, which ocf.AttrURL handles.
func ResolveHref(baseDir, href string) string {
	ref, err := url.Parse(href)
	if err != nil {
		return path.Join(baseDir, href) // malformed reference: best-effort literal join
	}
	root := path.Clean("/" + baseDir)
	if root != "/" {
		root += "/"
	}
	resolved := (&url.URL{Path: root}).ResolveReference(ref)
	return strings.TrimPrefix(resolved.Path, "/")
}
