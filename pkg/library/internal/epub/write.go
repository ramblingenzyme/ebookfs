package epub

import (
	"github.com/ramblingenzyme/ebookfs/internal/book"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

// Rewrite applies e to the epub at epubPath atomically. Every refusal runs
// before anything is written, so the original survives an error. It returns the
// Bib read back from the file, except when e has no edits at all and b.Bib is
// returned untouched.
//
// An edit asking for what the file already says skips the zip rebuild but is
// still re-read, and refusals apply before that is known.
//
// b is used only for validation and to locate the cover entry; its EpubPath is
// not read, so this package never resolves against the store root.
func Rewrite(epubPath string, b *book.Book, e book.Edits) (book.Bib, error) {
	if !e.HasCoverEdit() && !e.HasBibEdits() {
		return b.Bib, nil
	}

	// Backstop: Library.Edit is the enforcement point, and an unvalidated book.Edits
	// must never reach a file.
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
		// Before the write, not after it: the epub package has no opinion on a
		// book with no title. A sort title written against a package carrying no
		// dc:title mints an empty one, which would otherwise be caught only
		// once the original had been replaced.
		if err := usable(f); err != nil {
			return book.Bib{}, err
		}
	}
	if err := f.Save(); err != nil {
		return book.Bib{}, err
	}

	// Read back from the file rather than trusting the Bib the caller handed
	// in: library.Edit builds that from the index, which can disagree with the
	// epub, and an edit is an occasion to reconcile it. Save leaves the Book
	// reading the rewritten file, so this costs no second parse.
	bib, err := bib(f)
	if err != nil {
		return book.Bib{}, err
	}
	return *bib, nil
}

// apply assigns the fields e names. Unwrapping the pointers is book.Edits' business,
// not the book's: a nil means the edit did not name the field, which is an
// encoding this package chose.
func apply(f *epubfile.Book, e book.Edits) {
	if e.Title != nil {
		f.Title = *e.Title
		// A retitled book drops the sort title it carried, which was derived
		// from the old title. Stated here rather than hidden in a setter,
		// because it is ebookfs's rule and not the format's.
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

// series folds a half-named series edit onto the membership the file records,
// and returns nil for one to clear. cur is what a reader was shown, so an
// index-only edit moves the book the reader saw rather than inventing a
// collection. A book in no series has no position to set, so the edit is
// dropped rather than minting an empty one.
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
