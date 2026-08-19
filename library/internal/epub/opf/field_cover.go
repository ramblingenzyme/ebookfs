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

// isRasterCoverType rejects markup "cover pages" the way calibre does: an empty
// media-type, or one containing "xml" or "html".
func isRasterCoverType(mediaType string) bool {
	mt := strings.ToLower(mediaType)
	return mt != "" && !strings.Contains(mt, "xml") && !strings.Contains(mt, "html")
}

// cover resolves the cover image: the cover-image manifest property first, then
// the legacy <meta name="cover"> (§5.9.3 fixes that order), then a heuristic.
// The order of the loops is the whole rule.
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

	// Heuristic fallback, described by neither spec.
	//
	// ponytail: last match wins, where calibre takes the first. Revisit if a
	// book shows the wrong cover.
	found := ""
	for _, item := range manifest {
		if strings.HasPrefix(item.MediaType, "image") &&
			strings.Contains(strings.ToLower(item.ID), "cover") {
			found = coverUrl(base, item.Href)
		}
	}
	return found
}
