package opf

import (
	"net/url"
	"path"
	"strings"
)

func coverUrl(baseDir, href string) string {
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

// isRasterCoverType reports whether a manifest media-type denotes a raster cover
// image rather than a markup "cover page". Mirrors calibre, which rejects any
// cover whose media-type is empty or contains "xml"/"html" (e.g. an XHTML cover
// page mislabelled with properties="cover-image").
func isRasterCoverType(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return mt != "" && !strings.Contains(mt, "xml") && !strings.Contains(mt, "html")
}

// translateCover resolves the cover image, in the order §5.9.3 requires: the
// cover-image manifest property is the EPUB 3 mechanism and wins, and the
// legacy <meta name="cover"> exists only "to identify a cover image for EPUB 2
// reading systems". The order of the first two loops below *is* that rule —
// swapping them silently flips the result, so TestSpecCoverImagePropertyBeatsLegacyMeta
// pins it.
//
// The third loop is a heuristic with no basis in either spec, and it runs even
// when a legacy cover meta is present but names an id no manifest item has: a
// broken pointer must not do worse than a missing one. Calibre keeps looking
// too.
func (o *Doc) cover(base string) string {
	manifest := o.manifest()

	for _, item := range manifest {
		if strings.Contains(item.Properties, "cover-image") && isRasterCoverType(item.MediaType) {
			return coverUrl(base, item.Href)
		}
	}

	coverID := o.namedMeta("cover")
	if coverID != "" {
		for _, item := range manifest {
			if item.ID == coverID && isRasterCoverType(item.MediaType) {
				return coverUrl(base, item.Href)
			}
		}
	}

	// Heuristic fallback: last image item whose id contains "cover". Neither
	// spec describes this; it is a guess for files that name no cover at all.
	//
	// ponytail: last-match-wins, where calibre takes the first, so a file with
	// several cover-ish ids can disagree with calibre about which is the cover.
	// Revisit if a book shows the wrong cover and the manifest has more than one
	// matching id.
	found := ""
	for _, item := range manifest {
		if strings.HasPrefix(item.MediaType, "image") &&
			strings.Contains(strings.ToLower(item.ID), "cover") {
			found = coverUrl(base, item.Href)
		}
	}
	return found
}
