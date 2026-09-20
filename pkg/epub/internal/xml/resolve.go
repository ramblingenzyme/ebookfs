package xml

import (
	"net/url"
	"path"
	"strings"
)

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
