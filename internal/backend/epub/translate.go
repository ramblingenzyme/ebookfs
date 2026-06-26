package epub

import (
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/backend/naming"
)

func translate(pkg *opfPackage) (*Book, error) {
	if pkg == nil {
		return nil, errors.New("nil package")
	}

	b := &Book{}

	if err := translateTitle(&pkg.Metadata, b); err != nil {
		return nil, err
	}

	if err := translateAuthor(&pkg.Metadata, b); err != nil {
		return nil, err
	}

	translateSeries(&pkg.Metadata, b)
	translateCover(pkg, b)

	b.Description = strings.TrimSpace(pkg.Metadata.Description)
	b.Identifiers = pkg.Metadata.Identifiers
	translateLanguage(&pkg.Metadata, b)
	translateDate(&pkg.Metadata, b)

	return b, nil
}

// findRefine resolves an EPUB 3 refinement: a <meta> whose property is the one
// requested and whose refines points at the given element id. The refines
// attribute carries a leading '#' fragment ("#id") that the id values we hold
// (from id attributes) lack, so it is trimmed before comparison.
func findRefine(meta []opfMeta, id, property string) string {
	for _, m := range meta {
		if m.Property == property && strings.TrimPrefix(m.Refines, "#") == id {
			return m.Value
		}
	}

	return ""
}

func translateLanguage(meta *opfMetadata, b *Book) {
	for _, l := range meta.Languages {
		if l = strings.TrimSpace(l); l != "" {
			b.Language = l
			return
		}
	}
}

// translateDate parses the first usable <dc:date>. EPUB dates are nominally
// ISO 8601 but vary in precision, so a short list of layouts is tried from most
// to least precise; an unparseable date is left zero rather than failing.
func translateDate(meta *opfMetadata, b *Book) {
	for _, d := range meta.Dates {
		if t, ok := parseEpubDate(d); ok {
			b.PubDate = t
			return
		}
	}
}

func parseEpubDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

func translateTitle(meta *opfMetadata, b *Book) error {
	if len(meta.Titles) == 0 {
		return errors.New("no title")
	}

	title := meta.Titles[0]

	var err error
	b.Title, err = naming.Sanitize(title.Value)
	if err != nil {
		return errors.New("empty title")
	}

	b.SortTitle = findRefine(meta.Metas, title.ID, "file-as")
	// TODO: decide whether to add sort title heuristic when not found

	return nil
}

func translateAuthor(meta *opfMetadata, b *Book) error {
	for _, c := range meta.Creators {
		role := c.Role
		if role == "" {
			role = findRefine(meta.Metas, c.ID, "role")
		}

		if role == "" {
			role = "aut"
		}

		if role != "aut" {
			continue
		}

		name, err := naming.Sanitize(c.Name)
		if err != nil {
			continue
		}

		sortAs := c.FileAs
		if sortAs == "" {
			sortAs = findRefine(meta.Metas, c.ID, "file-as")
		}
		if sortAs != "" {
			sortAs, _ = naming.Sanitize(sortAs)
		}

		b.Authors = append(b.Authors, Author{Name: name, SortAs: sortAs})
	}
	if len(b.Authors) == 0 {
		return errors.New("no authors")
	}

	return nil
}

func translateSeries(meta *opfMetadata, b *Book) {
	var collectionID string
	for _, m := range meta.Metas {
		if m.Property == "belongs-to-collection" {
			b.Series = strings.TrimSpace(m.Value)
			collectionID = m.ID
			break
		}

		if m.Name == "calibre:series" {
			b.Series = strings.TrimSpace(m.Content)
		}
		if m.Name == "calibre:series_index" {
			if idx, err := strconv.ParseFloat(m.Content, 64); err == nil {
				b.SeriesIndex = idx
			}
		}
	}

	if collectionID != "" {
		// Reset in case we pulled it from calibre
		b.SeriesIndex = 0

		pos := findRefine(meta.Metas, collectionID, "group-position")
		if pos != "" {
			if index, err := strconv.ParseFloat(pos, 64); err == nil {
				b.SeriesIndex = index
			}
		}
	}
}

// coverUrl resolves a manifest href into the literal zip entry path of the cover.
//
// Manifest href values are URI references relative to the OPF document, per
// OPF 2.0 §2.3 (idpf.org/epub/20/spec/OPF_2.0_final_spec.html) and EPUB 3.3
// Package Document §3.4.7 (w3.org/TR/epub-33/). They may therefore be
// percent-encoded ("cover%20image.jpg") or container-root-absolute
// ("/images/cover.jpg"), neither of which path.Join handles.
//
// net/url's ResolveReference performs RFC 3986 reference resolution against the
// OPF's directory (rooted at the container root, "/"): it is the standards-
// correct analogue of Node's path.resolve — an absolute href ignores baseDir —
// and additionally removes '.'/'..' segments and decodes percent-escapes so the
// result matches the literal name stored in the zip.
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

func translateCover(pkg *opfPackage, b *Book) {
	for _, item := range pkg.Manifest {
		if strings.Contains(item.Properties, "cover-image") && isRasterCoverType(item.MediaType) {
			b.CoverPath = coverUrl(pkg.BasePath, item.Href)
			return
		}
	}

	var coverID string
	for _, m := range pkg.Metadata.Metas {
		if m.Name == "cover" && m.Content != "" {
			coverID = m.Content
			break
		}
	}

	for _, item := range pkg.Manifest {
		if item.ID == coverID {
			if isRasterCoverType(item.MediaType) {
				b.CoverPath = coverUrl(pkg.BasePath, item.Href)
				return
			}
			continue
		}

		var (
			img   bool = strings.HasPrefix(item.MediaType, "image")
			cover bool = strings.Contains(strings.ToLower(item.ID), "cover")
		)

		if coverID == "" && cover && img {
			b.CoverPath = coverUrl(pkg.BasePath, item.Href)
		}
	}
}
