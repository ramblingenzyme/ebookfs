package opf

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The two things read out of the manifest that are not fields: where the NCX
// lives, and which document displays the cover.

// Required of the NCX item by OPF 2.0 §2.4.1.2, and §3.2's core media type
// table names it too.
const ncxMediaType = "application/x-dtbncx+xml"

// NCXPath returns the container path of the NCX, or "". base is the OPF's own
// directory, as for Bib.
//
// Found by media type rather than through <spine toc="…">, since §5.7.1 demotes
// that attribute to optional and legacy while the media type is required either
// way.
func (o *Doc) NCXPath(base string) string {
	for _, item := range o.d.Manifest() {
		if item.MediaType == ncxMediaType {
			return xml.ResolveHref(base, item.Href)
		}
	}
	return ""
}

// CoverPages returns the documents the package points at as displaying the
// cover, best pointer first: the legacy <guide> reference (OPF 2.0 §2.6, kept
// by §5.9.4), then the first spine item. Both are candidates rather than
// answers — the caller confirms one by finding the cover image inside it.
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
		// §2.6 fixes the type as "cover" case-sensitively, but producers
		// disagree often enough that matching exactly would only miss it.
		if strings.EqualFold(r.Type, "cover") {
			add(r.Href)
		}
	}
	add(o.firstSpineHref())
	return out
}

// firstSpineHref is the href of the first document in the reading order, or "".
// An idref the spine does not carry matches nothing, rather than the manifest
// item that also has none.
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
