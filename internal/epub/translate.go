package epub

import (
	"errors"
	"path"
	"strconv"
	"strings"
)

// Sanitize makes s safe for use as a filesystem path component.
// It replaces '/' with '-', strips NUL and control characters (< 0x20),
// and trims leading/trailing dots, spaces, and tabs.
// Returns an error if the result is empty.
func Sanitize(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/':
			b.WriteRune('-')
		case r < 0x20:
			// strip NUL and control characters
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ". \t")
	if out == "" {
		return "", errors.New("sanitized string is empty")
	}
	return out, nil
}

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

	return b, nil
}

func findRefine(meta []opfMeta, id, property string) string {
	for _, m := range meta {
		if m.Name == property && m.Refines == id {
			return m.Value
		}
	}

	return ""
}

func translateTitle(meta *opfMetadata, b *Book) error {
	if len(meta.Titles) == 0 {
		return errors.New("no title")
	}

	title := meta.Titles[0]

	var err error
	b.Title, err = Sanitize(title.Value)
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

		name, err := Sanitize(c.Name)
		if err != nil {
			continue
		}

		sortAs := c.FileAs
		if sortAs == "" {
			sortAs = findRefine(meta.Metas, c.ID, "file-as")
		}
		if sortAs != "" {
			sortAs, _ = Sanitize(sortAs)
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
			if idx, err := strconv.ParseInt(m.Content, 10, 16); err == nil {
				b.SeriesIndex = uint16(idx)
			}
		}
	}

	if collectionID != "" {
		// Reset in case we pulled it from calibre
		b.SeriesIndex = 0

		pos := findRefine(meta.Metas, collectionID, "group-position")
		if pos != "" {
			if index, err := strconv.ParseInt(pos, 10, 16); err == nil {
				b.SeriesIndex = uint16(index)
			}
		}
	}
}

func coverUrl(base, href string) string {
	// Manifest href values are relative to the OPF document itself, per
	// OPF 2.0 §2.3 (idpf.org/epub/20/spec/OPF_2.0_final_spec.html) and
	// EPUB 3.3 Package Document §3.4.7 (w3.org/TR/epub-33/).
	// TODO: handle absolute paths, as per the epub standard
	return path.Join(base, href)
}

func translateCover(pkg *opfPackage, b *Book) {
	for _, item := range pkg.Manifest {
		if strings.Contains(item.Properties, "cover-image") {
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
			b.CoverPath = coverUrl(pkg.BasePath, item.Href)
			return
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
