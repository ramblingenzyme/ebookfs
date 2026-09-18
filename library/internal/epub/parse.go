package epub

import (
	"archive/zip"
	"errors"
	"path"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/edits"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf"
)

func Parse(bpath string) (*book.Bib, error) {
	r, err := zip.OpenReader(bpath)
	if err != nil {
		return nil, notEpub(bpath, err)
	}
	defer r.Close()

	a, err := openArchive(&r.Reader)
	if err != nil {
		return nil, err
	}
	if err := a.validate(); err != nil {
		return nil, err
	}

	opfBytes, err := a.read(a.opf)
	if err != nil {
		return nil, err
	}
	doc, err := opf.Parse(opfBytes)
	if err != nil {
		return nil, err
	}

	bib, err := translate(doc.Metadata(path.Dir(a.opf)))
	if err != nil {
		return nil, err
	}

	// From the zip central directory, so nothing is decompressed. The epub's own
	// size is left to the library, which stats it for drift detection anyway.
	bib.OpfSize = a.size(a.opf)
	if bib.CoverPath != "" {
		bib.CoverSize = a.size(bib.CoverPath)
	}

	return bib, nil
}

// translate turns the package document's own reading of itself into the Bib
// ebookfs indexes, applying the two rules that are ebookfs's rather than the
// format's: a book must be usable, and a malformed series position must still
// display.
func translate(m opf.Metadata) (*book.Bib, error) {
	// ebookfs builds every path from the title and the authors, so a book
	// missing either cannot be filed. The file is free to omit them.
	if m.Title == "" {
		return nil, errors.New("no title")
	}
	if len(m.Authors) == 0 {
		return nil, errors.New("no authors")
	}

	bib := &book.Bib{
		Title:       m.Title,
		SortTitle:   m.SortTitle,
		Description: m.Description,
		Language:    m.Language,
		Pubdate:     m.Pubdate,
		Identifiers: m.Identifiers,
		CoverPath:   m.CoverPath,
	}
	for _, a := range m.Authors {
		bib.Authors = append(bib.Authors, book.Author{Name: a.Name, SortName: a.SortName})
	}
	if m.Series != nil {
		// Defaulted on the way in, not in the document, so a rewrite cannot
		// write it back.
		index := m.Series.Index
		if !edits.ValidSeriesIndex(index) {
			index = "1"
		}
		bib.Series = &book.SeriesRef{Name: m.Series.Name, Index: index}
	}
	return bib, nil
}
