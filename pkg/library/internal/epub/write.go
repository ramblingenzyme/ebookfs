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

	if err := edit(f, e); err != nil {
		return book.Bib{}, err
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

// edit applies e to f in memory.
func edit(f *epubfile.Book, e book.Edits) error {
	if e.HasCoverEdit() {
		if err := f.SetCover(*e.Cover); err != nil {
			return err
		}
	}

	if !e.HasBibEdits() {
		return nil
	}

	apply(f, e)
	// Checked before Save, since setting a sort title on a package with no
	// dc:title adds an empty title element.
	return usable(f)
}

func apply(f *epubfile.Book, e book.Edits) {
	for _, field := range []struct {
		changed bool
		set     func()
	}{
		// Title runs before SortTitle, since a new title clears the sort title
		// that a SortTitle edit then sets.
		{e.Title != nil && *e.Title != f.Title, func() { f.Title, f.SortTitle = *e.Title, "" }},
		{e.SortTitle != nil, func() { f.SortTitle = *e.SortTitle }},
		{e.Description != nil, func() { f.Description = *e.Description }},
		{e.Language != nil, func() { f.Language = *e.Language }},
		{e.Publisher != nil, func() { f.Publisher = *e.Publisher }},
		{e.Rights != nil, func() { f.Rights = *e.Rights }},
		{e.Authors != nil, func() { f.Authors = authors(*e.Authors) }},
		{e.Series != nil || e.SeriesIndex != nil, func() { f.Series = series(f.Series, e) }},
		{e.Subjects != nil, func() { f.Subjects = *e.Subjects }},
		{e.Contributors != nil, func() { f.Contributors = contributors(*e.Contributors) }},
	} {
		if field.changed {
			field.set()
		}
	}
}

func authors(as []book.Author) []epubfile.Author {
	out := make([]epubfile.Author, len(as))
	for i, a := range as {
		out[i] = epubfile.Author{Name: a.Name, SortName: a.SortName}
	}

	return out
}

func contributors(cs []book.Contributor) []epubfile.Contributor {
	out := make([]epubfile.Contributor, len(cs))
	for i, c := range cs {
		out[i] = epubfile.Contributor{Name: c.Name, Role: c.Role}
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
