package opf

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The manifest, and the one thing this package reads out of it that is not a
// field: where the NCX lives.

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

// ncxMediaType is what both specs make the NCX item carry: OPF 2.0 §2.4.1.2
// requires it, and §3.2's core media type table names it as the legacy NCX's.
const ncxMediaType = "application/x-dtbncx+xml"

// NCXPath returns the container path of the NCX, or "" if the package declares
// none. base is the OPF's own directory, as for Bib.
//
// Found by media type rather than through <spine toc="…">, which OPF 2.0
// §2.4.1.2 makes the pointer to it: §5.7.1 demotes that attribute to optional
// and legacy, so an EPUB 3 package keeping an NCX for older reading systems may
// carry the item without the pointer. The media type is required either way.
func (o *Doc) NCXPath(base string) string {
	for _, item := range o.manifest() {
		if item.MediaType == ncxMediaType {
			return xml.ResolveHref(base, item.Href)
		}
	}
	return ""
}

// CoverPages returns the container paths of the documents the package points at
// as the one displaying the cover, best pointer first. base is the OPF's own
// directory, as for Bib.
//
// The pointers are the legacy <guide> reference (OPF 2.0 §2.6, kept by §5.9.4),
// and failing that the first spine item — where a cover page has to be to be
// the first thing seen. Both are candidates rather than answers: the caller
// confirms one by finding the cover image referenced inside it.
//
// ponytail: the EPUB 3 replacement for the guide, the landmarks nav (§7.4.4),
// is not consulted; reaching it means finding and parsing the navigation
// document. Add it if a book turns up whose cover page neither pointer reaches.
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
			// §2.6 fixes the type as "cover", and the OPF 2.0 grammar is
			// case-sensitive, but producers disagree often enough that matching
			// exactly would miss the reference rather than protect anything.
			if strings.EqualFold(attr(r, "type"), "cover") {
				add(r.SelectAttrValue("href", ""))
			}
		}
	}
	add(o.firstSpineHref())
	return out
}

// firstSpineHref returns the href of the first document in the reading order,
// or "" if the package declares no usable spine.
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
