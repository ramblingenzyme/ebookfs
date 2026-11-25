package opf

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

// isRasterCoverType rejects markup cover pages as calibre does: an empty media
// type, or one containing "xml" or "html".
func isRasterCoverType(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return mt != "" && !strings.Contains(mt, "xml") && !strings.Contains(mt, "html")
}

// cover resolves the cover image: the cover-image manifest property first, then
// the legacy <meta name="cover"> (§5.9.3), then a heuristic.
func (o *Package) cover(base string) string {
	manifest := o.d.Manifest()

	for _, item := range manifest {
		if o.d.HasProperty(item.Properties, "cover-image") && isRasterCoverType(item.MediaType) {
			return xml.ResolveHref(base, item.Href)
		}
	}

	coverID := o.d.Named("cover").Get()
	if coverID != "" {
		for _, item := range manifest {
			if item.ID == coverID && isRasterCoverType(item.MediaType) {
				return xml.ResolveHref(base, item.Href)
			}
		}
	}

	return guessCover(manifest, base)
}

// guessCover takes an image item whose id mentions "cover". Neither spec
// describes this fallback.
//
// ponytail: last match wins, where calibre takes the first. Revisit if a
// book shows the wrong cover.
func guessCover(manifest []pkgdoc.Item, base string) string {
	found := ""

	for _, item := range manifest {
		if strings.HasPrefix(item.MediaType, "image") &&
			strings.Contains(strings.ToLower(item.ID), "cover") {
			found = xml.ResolveHref(base, item.Href)
		}
	}

	return found
}
