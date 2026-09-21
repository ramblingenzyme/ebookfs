// Package xml holds the three rules an epub's XML obeys wherever it appears:
// the whitespace collapse a reader owes every value, the resolution of an href
// against the document carrying it, and the namespace prefix a created element
// takes.
//
// Each crosses a package boundary, which is what puts it here. A rule only one
// document format needs belongs with that format, as the container's attribute
// types do (internal/ocf).
package xml

import (
	"net/url"
	"path"
	"strings"
)

// Collapse is the whitespace normalization XML 1.0 §3.3.3 requires of a reader.
// Neither encoding/xml nor etree applies it.
func Collapse(s string) string { return strings.Join(strings.Fields(s), " ") }

// Qualify joins an xmlns prefix to a local name, so an element created beside a
// sibling lands in the same namespace the file already uses. The empty prefix
// is the default namespace and needs no colon.
//
// An xmlns prefix only. A vocabulary prefix, the "dcterms:" in a property
// value, is a different thing that resolves through the package document's own
// prefix mappings.
func Qualify(prefix, tag string) string {
	if prefix == "" {
		return tag
	}
	return prefix + ":" + tag
}

// ResolveHref turns an href from inside the container into the container path
// of the entry it names, resolved against the directory of the document that
// carries it. Any fragment is dropped: a reference into a document still names
// that document.
//
// The result is the decoded form alone, the one a conforming file means. A
// producer that wrote an unencoded name into both the XML and the zip is what
// AttrURL exists for; a caller that looks the result up as a zip entry rather
// than comparing it against another resolved path inherits that limitation.
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
