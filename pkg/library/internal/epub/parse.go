// Package epub translates between pkg/epub and the library's book model. It
// also applies four rules of ebookfs's own:
//
//   - A book needs a title and at least one author, because the store builds
//     every path from them.
//   - A series position the spec disallows is read as 1. The file is not
//     changed.
//   - Changing a book's title clears its sort title, which described the old
//     title.
//   - A repeated author, subject or contributor-in-role is read once. The
//     index keys each on (book, entry), and the store would name a repeated
//     author twice in the book's directory.
package epub

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

func Parse(bpath string) (*book.Bib, error) {
	b, err := epubfile.Open(bpath)
	if err != nil {
		return nil, err
	}
	defer b.Close()
	return bib(b)
}

func bib(b *epubfile.Book) (*book.Bib, error) {
	if err := usable(b); err != nil {
		return nil, err
	}

	bib := &book.Bib{
		Title:       b.Title,
		SortTitle:   b.SortTitle,
		Description: b.Description,
		Language:    b.Language,
		Publisher:   b.Publisher,
		Rights:      b.Rights,
		Pubdate:     b.Pubdate(),
		Subjects:    book.FirstOf(b.Subjects, func(s string) string { return s }),
		Identifiers: b.Identifiers(),
		CoverPath:   b.CoverPath(),
		// EpubSize is left to the library, which already stats the file.
		OpfSize: b.Size(b.PackagePath()),
	}
	for _, a := range b.Authors {
		bib.Authors = append(bib.Authors, book.Author{Name: a.Name, SortName: a.SortName})
	}
	bib.Authors = book.FirstOf(bib.Authors, func(a book.Author) string { return a.Name })
	for _, c := range b.Contributors {
		bib.Contributors = append(bib.Contributors, book.Contributor{Name: c.Name, Role: c.Role})
	}
	bib.Contributors = book.FirstOf(bib.Contributors, func(c book.Contributor) book.Contributor { return c })
	if bib.CoverPath != "" {
		bib.CoverSize = b.Size(bib.CoverPath)
	}
	if b.Series != nil {
		index := b.Series.Index
		if !book.ValidSeriesIndex(index) {
			index = "1"
		}
		bib.Series = &book.SeriesRef{Name: b.Series.Name, Index: index}
	}
	return bib, nil
}

func usable(b *epubfile.Book) error {
	if b.Title == "" {
		return errors.New("no title")
	}
	if len(b.Authors) == 0 {
		return errors.New("no authors")
	}
	return nil
}
