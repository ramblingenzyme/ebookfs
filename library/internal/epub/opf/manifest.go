package opf

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The manifest, and the two things read out of it that are not fields: where the
// NCX lives, and which document displays the cover.

func (o *Doc) manifest() []manifestItem {
	m := o.pkg.SelectElement("manifest")
	if m == nil {
		return nil
	}
	var out []manifestItem
	for _, it := range m.SelectElements("item") {
		out = append(out, manifestItem{
			ID: attr(it, "id"),
			// Only trimmed, not collapsed: href is a percent-encoded URL, and
			// collapsing could rewrite a literal filename.
			Href:       strings.TrimSpace(it.SelectAttrValue("href", "")),
			MediaType:  attr(it, "media-type"),
			Properties: attr(it, "properties"),
		})
	}
	return out
}

type manifestItem struct {
	ID         string
	Href       string
	MediaType  string
	Properties string
}

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
	for _, item := range o.manifest() {
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
		href = strings.TrimSpace(href)
		if href == "" {
			return
		}
		if p := xml.ResolveHref(base, href); p != "" && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}

	if g := o.pkg.SelectElement("guide"); g != nil {
		for _, r := range g.SelectElements("reference") {
			// §2.6 fixes the type as "cover" case-sensitively, but producers
			// disagree often enough that matching exactly would only miss it.
			if strings.EqualFold(attr(r, "type"), "cover") {
				add(r.SelectAttrValue("href", ""))
			}
		}
	}
	add(o.firstSpineHref())
	return out
}

// firstSpineHref is the href of the first document in the reading order, or "".
func (o *Doc) firstSpineHref() string {
	spine := o.pkg.SelectElement("spine")
	if spine == nil {
		return ""
	}
	first := spine.SelectElement("itemref")
	if first == nil {
		return ""
	}
	idref := attr(first, "idref")
	for _, item := range o.manifest() {
		if item.ID == idref {
			return item.Href
		}
	}
	return ""
}
