package epub

import (
	"path"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf"
)

// Book is an open epub with its metadata. Change a field and call Save to
// write it. A Book is not safe for concurrent use.
type Book struct {
	*File

	Title       string
	SortTitle   string // "" if the file has none; set "" to remove it
	Authors     []Author
	Series      *Series // nil if the book is in no series; set nil to remove it
	Description string  // "" is an empty element, not a missing one
	Language    string

	pubdate     string
	doc         *opf.Doc
	identifiers map[string]string
	coverPath   string
	// orig is a deep copy, so changing an exported slice cannot change what
	// Save compares against.
	orig snapshot
	// cover is held until Save, so a cover and a metadata change rebuild the
	// archive only once.
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

// Open opens the epub at path and parses its package document. A book is never
// rejected for what its metadata says.
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

// Pubdate returns the publication date exactly as the file writes it. It is ""
// if the file has none, or has several and none can be chosen.
func (b *Book) Pubdate() string { return b.pubdate }

// Identifiers returns the book's identifiers keyed by scheme, such as "isbn" or
// "uuid". One with no scheme is keyed by its XML id, or else by "unknown",
// "unknown-2" and so on (docs/DECISIONS.md #24).
//
// Identifiers are read-only, since they state the book's published identity.
// Changing the map does not change the file.
func (b *Book) Identifiers() map[string]string { return b.identifiers }

// CoverPath returns the cover image's path inside the zip, or "".
func (b *Book) CoverPath() string { return b.coverPath }

// Cover returns the cover image, or ErrNoCover if the book has none.
func (b *Book) Cover() ([]byte, error) {
	if b.coverPath == "" {
		return nil, ErrNoCover
	}
	return b.ReadEntry(b.coverPath)
}
