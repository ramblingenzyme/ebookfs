package epub

import (
	"errors"
	"net/url"
	"path"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
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

// translateDate resolves the publication date from the <dc:date> elements. In
// EPUB 2 these may be tagged with an opf:event ("publication"/"creation"/
// "modification" — the spec's example vocabulary). Publication-date selection
// is independent of parseability — we store the raw value as-is.
//
//  1. A designated opf:event="publication" date is authoritative (first match
//     if a malformed file declares several) and is stored verbatim.
//  2. Otherwise every evented date is the file declaring "this is not the
//     publication date", leaving only untagged <dc:date>. Exactly one untagged
//     date is used; zero or several leaves the date unset.
//
// EPUB 3 carries a single untagged <dc:date> (last-modified lives in a separate
// <meta property="dcterms:modified">, not a <dc:date>, so it never reaches here),
// falling through to the step-2 single-date case. Empty <dc:date> elements are
// ignored throughout.
func translateDate(meta *opfMetadata, b *Book) {
	var (
		untagged string
		count    int
	)
	for _, d := range meta.Dates {
		val := strings.TrimSpace(d.Value)
		event := strings.TrimSpace(d.Event)
		if val == "" {
			continue
		}
		if strings.ToLower(event) == "publication" {
			b.PubDate = val
			return
		}
		if event == "" {
			count++
			untagged = val
		}
	}
	if count == 1 {
		b.PubDate = untagged
	}
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

	// The title sort is the EPUB 3 file-as refine on the title; EPUB 2 has no
	// standard mechanism, so it is simply absent there.
	b.SortTitle = findRefine(meta.Metas, title.ID, "file-as")
	// TODO: decide whether to derive a sort title heuristically when none is set
	// (calibre strips leading articles, e.g. "The Hobbit" -> "Hobbit, The"); it is
	// language-dependent, so for now an unset sort title is left empty.

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

		// Non-author contributors (editors, illustrators, translators) are
		// parsed correctly but deliberately excluded: the frontend has no
		// concept of contributor roles — the 9P authors field is a flat list
		// of names — and WriteBib's setAuthors leaves non-aut creators
		// untouched in the OPF, so exposing them would create a broken
		// round-trip where removing an editor from the 9P authors field
		// appears to work but the editor survives in the epub.
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

		b.Authors = append(b.Authors, model.Author{Name: name, SortName: sortAs})
	}
	if len(b.Authors) == 0 {
		return errors.New("no authors")
	}

	return nil
}

// translateSeries resolves the series name and index, mirroring calibre's
// read_series precedence. An EPUB 3 belongs-to-collection wins, but only when it
// is genuinely a series: it must carry an id (needed to resolve its refines) and
// a collection-type refine of "series". A "set" collection (e.g. a publisher
// bundle) is skipped rather than mistaken for a series, so a legitimate
// calibre:series can still be found. Failing an EPUB 3 series, the proprietary
// EPUB 2 calibre:series / calibre:series_index metas are used.
//
// The index defaults to 1 (calibre's convention) whenever a series is present
// but carries no parseable position — a 0 would surface as "0. Title" in the
// by-series view and sort ahead of the real, numbered entries.
func translateSeries(meta *opfMetadata, b *Book) {
	b.SeriesIndex = 1

	for _, m := range meta.Metas {
		if m.Property != "belongs-to-collection" || m.ID == "" {
			continue
		}
		name := strings.TrimSpace(m.Value)
		if name == "" || findRefine(meta.Metas, m.ID, "collection-type") != "series" {
			continue
		}
		b.Series = name
		if pos := findRefine(meta.Metas, m.ID, "group-position"); pos != "" {
			if idx, err := strconv.ParseFloat(pos, 64); err == nil {
				b.SeriesIndex = idx
			}
		}
		return
	}

	for _, m := range meta.Metas {
		switch m.Name {
		case "calibre:series":
			b.Series = strings.TrimSpace(m.Content)
		case "calibre:series_index":
			if idx, err := strconv.ParseFloat(m.Content, 64); err == nil {
				b.SeriesIndex = idx
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
