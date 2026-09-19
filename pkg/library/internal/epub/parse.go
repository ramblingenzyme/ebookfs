// Package epub adapts the standalone epub package to the library's model. The
// file format lives there; this package translates, and holds the rules that
// are ebookfs's rather than the format's.
//
// Three of them. A book needs a title and authors to be filed, because the
// store builds every path from both, so Parse refuses one missing either while
// the format package reports what the file says. A series position the spec
// disallows still has to display, so it is defaulted on the way in and never
// written back. And a retitled book drops the sort title it carried, which
// described the old title.
//
// On the write side the job is unwrapping book.Edits, whose nil field means the
// caller did not name it. That encoding is the library's rather than the
// format's, so the epub package is handed values and never pointers.
package epub

import (
	"errors"

	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Parse reads the epub's metadata into the Bib the library indexes.
func Parse(bpath string) (*book.Bib, error) {
	b, err := epubfile.Open(bpath)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	return bib(b)
}

// bib turns what the file says about itself into the Bib ebookfs indexes,
// applying the two rules that are ebookfs's rather than the format's: a book
// must be usable, and a malformed series position must still display.
func bib(b *epubfile.Book) (*book.Bib, error) {
	if err := usable(b); err != nil {
		return nil, err
	}

	bib := &book.Bib{
		Title:       b.Title,
		SortTitle:   b.SortTitle,
		Description: b.Description,
		Language:    b.Language,
		Pubdate:     b.Pubdate(),
		Identifiers: b.Identifiers(),
		CoverPath:   b.CoverPath(),
		// From the zip central directory, so nothing is decompressed. The
		// epub's own size is left to the library, which stats it for drift
		// detection anyway.
		OpfSize: b.Size(b.PackagePath()),
	}
	for _, a := range b.Authors {
		bib.Authors = append(bib.Authors, book.Author{Name: a.Name, SortName: a.SortName})
	}
	if bib.CoverPath != "" {
		bib.CoverSize = b.Size(bib.CoverPath)
	}
	if b.Series != nil {
		// Defaulted on the way in, not in the document, so a rewrite cannot
		// write it back.
		index := b.Series.Index
		if !book.ValidSeriesIndex(index) {
			index = "1"
		}
		bib.Series = &book.SeriesRef{Name: b.Series.Name, Index: index}
	}
	return bib, nil
}

// usable reports whether the book can be filed. ebookfs builds every path from
// the title and the authors, so a book missing either has nowhere to live; the
// epub package reports both as the file states them, because an epub is free to
// omit them.
func usable(b *epubfile.Book) error {
	if b.Title == "" {
		return errors.New("no title")
	}
	if len(b.Authors) == 0 {
		return errors.New("no authors")
	}
	return nil
}
