package epub

import (
	"path"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf"
)

// Book is an open epub and its package document's metadata. Assign a field and
// call Save; a field left alone is left alone in the file. A Book is not safe
// for concurrent use.
type Book struct {
	*File

	Title       string
	SortTitle   string // "" when the file states none, and "" to remove it
	Authors     []Author
	Series      *Series // nil when the book is in none, and nil to remove it
	Description string  // "" is an empty element, not an absent one
	Language    string

	pubdate     string
	doc         *opf.Doc
	identifiers map[string]string
	coverPath   string
	// orig is deep-copied, so a caller mutating an exported slice cannot move
	// the baseline Save diffs against.
	orig snapshot
	// cover is staged until Save, so a cover and a metadata edit rebuild the
	// archive once.
	cover []byte
}

type snapshot struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *Series
	Description string
	Language    string
}

// Open opens the epub at path and parses its package document. The caller
// closes the returned Book, which is non-nil iff err is nil. Nothing is
// rejected for its contents.
func Open(p string) (_ *Book, err error) {
	f, err := OpenFile(p)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	raw, err := f.ReadEntry(f.PackagePath())
	if err != nil {
		return nil, err
	}
	doc, err := opf.Parse(raw)
	if err != nil {
		return nil, err
	}
	return newBook(f, doc), nil
}

func newBook(f *File, doc *opf.Doc) *Book {
	m := doc.Metadata(path.Dir(f.PackagePath()))

	b := &Book{File: f, doc: doc, identifiers: m.Identifiers, coverPath: m.CoverPath}
	b.Title = m.Title
	b.SortTitle = m.SortTitle
	b.Description = m.Description
	b.Language = m.Language
	b.pubdate = m.Pubdate
	for _, a := range m.Authors {
		b.Authors = append(b.Authors, Author(a))
	}
	b.Series = (*Series)(m.Series)
	b.orig = b.take()
	return b
}

func (b *Book) take() snapshot {
	s := snapshot{
		Title:       b.Title,
		SortTitle:   b.SortTitle,
		Authors:     slices.Clone(b.Authors),
		Description: b.Description,
		Language:    b.Language,
	}
	if b.Series != nil {
		series := *b.Series
		s.Series = &series
	}
	return s
}

// Pubdate returns the publication date as the file states it, unparsed. It is
// "" when the file states none or several ambiguously.
func (b *Book) Pubdate() string { return b.pubdate }

// Identifiers returns the book's identifiers keyed by scheme ("isbn", "uuid"),
// falling back to the element's XML id, then a numbered "unknown"
// (docs/DECISIONS.md #24). Read-only, since an identifier is a claim about the
// book's published identity. Mutating the map changes nothing in the file.
func (b *Book) Identifiers() map[string]string { return b.identifiers }

// CoverPath returns the cover image's zip-relative path, or "".
func (b *Book) CoverPath() string { return b.coverPath }

// Cover returns the cover image's bytes, or ErrNoCover.
func (b *Book) Cover() ([]byte, error) {
	if b.coverPath == "" {
		return nil, ErrNoCover
	}
	return b.ReadEntry(b.coverPath)
}
