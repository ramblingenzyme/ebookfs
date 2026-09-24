package epub

import (
	"github.com/ramblingenzyme/ebookfs/internal/book"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

// Rewrite applies e to the epub at epubPath and returns the book's metadata
// as read back from the file. If e names no field, it returns b.Bib without
// opening the file. Every check runs before anything is written.
//
// b.EpubPath is relative to the store root, so the caller passes the full path
// as epubPath.
func Rewrite(epubPath string, b *book.Book, e book.Edits) (book.Bib, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return b.Bib, nil
	}

	// Library.Edit validates e too. This catches a caller that skipped it.
	if v := book.Validate(e, b); v != nil {
		return book.Bib{}, v
	}

	f, err := epubfile.Open(epubPath)
	if err != nil {
		return book.Bib{}, err
	}
	defer f.Close()

	if e.HasCoverEdit() {
		if err := f.SetCover(*e.Cover); err != nil {
			return book.Bib{}, err
		}
	}
	if e.HasBibEdits() {
		apply(f, e)
		// Checked before Save, since setting a sort title on a package with no
		// dc:title adds an empty title element.
		if err := usable(f); err != nil {
			return book.Bib{}, err
		}
	}
	if err := f.Save(); err != nil {
		return book.Bib{}, err
	}

	// Read from the file rather than returning b, which came from the index
	// and may be out of date. Save has already loaded the rewritten file into
	// f, so this reads from memory.
	bib, err := bib(f)
	if err != nil {
		return book.Bib{}, err
	}
	return *bib, nil
}

func apply(f *epubfile.Book, e book.Edits) {
	if e.Title != nil {
		f.Title = *e.Title
		f.SortTitle = ""
	}
	if e.SortTitle != nil {
		f.SortTitle = *e.SortTitle
	}
	if e.Description != nil {
		f.Description = *e.Description
	}
	if e.Language != nil {
		f.Language = *e.Language
	}
	if e.Authors != nil {
		f.Authors = authors(*e.Authors)
	}
	if e.Series != nil || e.SeriesIndex != nil {
		f.Series = series(f.Series, e)
	}
}

func authors(as []book.Author) []epubfile.Author {
	out := make([]epubfile.Author, len(as))
	for i, a := range as {
		out[i] = epubfile.Author{Name: a.Name, SortName: a.SortName}
	}
	return out
}

// series applies a series edit to cur, the series the file records. An edit
// may set only the name or only the index, and the other is kept from cur. It
// returns nil, clearing the series, when the result has no name. That includes
// an index edit on a book in no series.
func series(cur *epubfile.Series, e book.Edits) *epubfile.Series {
	s := epubfile.Series{}
	if cur != nil {
		s = *cur
	}
	if e.Series != nil {
		s.Name = *e.Series
	}
	if e.SeriesIndex != nil {
		s.Index = *e.SeriesIndex
	}
	if s.Name == "" {
		return nil
	}
	return &s
}
