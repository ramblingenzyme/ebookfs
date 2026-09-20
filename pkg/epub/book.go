package epub

import (
	"path"
	"slices"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf"
)

// Book is an open epub and the metadata its package document carries. Assign a
// field and call Save; a field left alone is left alone in the file.
//
// The exported fields are what this package models and can write back.
// Everything else is a method: Identifiers and CoverPath because writing
// through them is meaningless, Cover because it costs I/O.
//
// A Book is not safe for concurrent use.
type Book struct {
	*File

	Title     string
	SortTitle string // "" when the file states none, and "" to remove it
	Authors   []Author
	Series    *Series // nil when the book is in none, and nil to remove it
	// Description is repeatable in the spec but single-valued here; a "" is a
	// legitimate empty element, not an absent one.
	Description string
	Language    string

	pubdate string
	doc     *opf.Doc
	// identifiers and coverPath are read once at Open: neither is writable, so
	// neither can go stale, and both cost a walk of the package document.
	identifiers map[string]string
	coverPath   string
	// orig is the metadata as parsed, deep-copied so a caller mutating an
	// exported slice or the Series it points at cannot move the baseline with
	// it. Save writes whatever differs from this.
	orig snapshot
	// cover is a staged image replacement, applied by the next Save. Staged
	// rather than written so that a cover edit and a metadata edit rebuild the
	// archive once between them.
	cover []byte
}

// snapshot is the comparable half of a Book: the fields Save diffs.
type snapshot struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *Series
	Description string
	Language    string
}

// Open opens the epub at path and parses its package document. The returned
// Book keeps the file open; the caller must Close it. It is non-nil iff err is
// nil.
//
// Nothing is rejected for its contents. A book with no title, no authors or a
// series position the spec does not allow opens successfully and reports what
// it carries, because whether that is usable is the caller's question.
func Open(p string) (*Book, error) {
	f, err := OpenFile(p)
	if err != nil {
		return nil, err
	}
	raw, err := f.ReadEntry(f.PackagePath())
	if err != nil {
		f.Close()
		return nil, err
	}
	doc, err := opf.Parse(raw)
	if err != nil {
		f.Close()
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

// take copies the diffed fields, deeply enough that nothing the caller can
// reach is shared with the result.
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

// Pubdate returns the publication date exactly as the file states it, never
// parsed, and "" when the file states none or states several ambiguously.
// Read-only: nothing here writes a date, so it is a method rather than a field.
func (b *Book) Pubdate() string { return b.pubdate }

// Identifiers returns the book's identifiers keyed by scheme ("isbn", "uuid",
// "doi") as the file states it, falling back to the element's XML id and then
// to a numbered "unknown" when nothing names it. Read-only: an identifier is a
// claim about the book's published identity, not a field to edit.
//
// The map is the Book's own; a caller mutating it changes nothing in the file.
func (b *Book) Identifiers() map[string]string { return b.identifiers }

// CoverPath returns the cover image's zip-relative path, or "" when the package
// document points at none. Derived from the manifest, so it is read-only.
func (b *Book) CoverPath() string { return b.coverPath }

// Cover returns the cover image's bytes, or ErrNoCover when the package
// document points at none.
func (b *Book) Cover() ([]byte, error) {
	if b.coverPath == "" {
		return nil, ErrNoCover
	}
	return b.ReadEntry(b.coverPath)
}
