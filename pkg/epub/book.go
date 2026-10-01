package epub

import (
	"path"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf"
)

// Metadata holds the editable bibliographic fields of an EPUB.
// It is embedded in Book so field access like b.Title works via promotion.
type Metadata struct {
	Title        string
	SortTitle    string // "" if the file has none; set "" to remove it
	Authors      []Author
	Series       *Series // nil if the book is in no series; set nil to remove it
	Description  string  // "" is an empty element, not a missing one
	Language     string
	Publisher    string
	Rights       string
	Subjects     []string
	Contributors []Contributor
}

// Book is an open epub with its metadata. Change a field and call Save to
// write it. A Book is not safe for concurrent use.
type Book struct {
	*File
	Metadata

	pubdate     string
	doc         *opf.Doc
	identifiers map[string]string
	coverPath   string
	// orig is a deep copy, so changing an exported slice cannot change what
	// Save compares against.
	orig Metadata
	// cover is held until Save, so a cover and a metadata change rebuild the
	// archive only once.
	cover []byte
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
	b.Publisher = m.Publisher
	b.Rights = m.Rights
	b.Subjects = m.Subjects
	b.pubdate = m.Pubdate
	b.Authors = make([]Author, 0, len(m.Authors))
	for _, a := range m.Authors {
		b.Authors = append(b.Authors, Author(a))
	}
	b.Contributors = make([]Contributor, 0, len(m.Contributors))
	for _, c := range m.Contributors {
		b.Contributors = append(b.Contributors, Contributor(c))
	}
	b.Series = (*Series)(m.Series)
	b.orig = b.take()
	return b
}

func (b *Book) take() Metadata {
	s := Metadata{
		Title:        b.Title,
		SortTitle:    b.SortTitle,
		Authors:      slices.Clone(b.Authors),
		Description:  b.Description,
		Language:     b.Language,
		Publisher:    b.Publisher,
		Rights:       b.Rights,
		Subjects:     slices.Clone(b.Subjects),
		Contributors: slices.Clone(b.Contributors),
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
