package opf

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// OPF 2.0 §2.4.1.2 requires this media type of the NCX item.
const ncxMediaType = "application/x-dtbncx+xml"

// NCXPath returns the NCX's path in the container, or "". base is as for
// Metadata.
//
// It matches the media type rather than <spine toc="…">, since §5.7.1 makes
// that attribute optional and legacy.
func (o *Doc) NCXPath(base string) string {
	for _, item := range o.d.Manifest() {
		if item.MediaType == ncxMediaType {
			return xml.ResolveHref(base, item.Href)
		}
	}
	return ""
}

// CoverPages returns the documents that may display the cover, best first: the
// legacy <guide> reference (OPF 2.0 §2.6, kept by §5.9.4), then the first
// spine item. The caller confirms one by finding the cover image inside it.
//
// ponytail: the landmarks nav (§7.4.4) is not consulted, which would mean
// finding and parsing the navigation document. Add it if a book turns up whose
// cover page neither pointer reaches.
func (o *Doc) CoverPages(base string) []string {
	var out []string
	add := func(href string) {
		if href == "" {
			return
		}
		if p := xml.ResolveHref(base, href); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}

	for _, r := range o.d.Guide() {
		// §2.6 fixes "cover" case-sensitively, but producers disagree on case.
		if strings.EqualFold(r.Type, "cover") {
			add(r.Href)
		}
	}
	add(o.firstSpineHref())
	return out
}

// firstSpineHref returns "" for a spine with no itemref, rather than matching a
// manifest item with no id.
func (o *Doc) firstSpineHref() string {
	idref := o.d.SpineFirst()
	if idref == "" {
		return ""
	}
	for _, item := range o.d.Manifest() {
		if item.ID == idref {
			return item.Href
		}
	}
	return ""
}
