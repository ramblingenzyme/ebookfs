// Package book defines the internal book record types used by store,
// index, and library. These types are not exposed to external consumers;
// the public library.Book wrapper provides read-only access.
package book

import (
	"maps"
	"path/filepath"
	"slices"
	"time"
)

// Author represents a book author with optional sort name for display.
type Author struct {
	ID       int64
	Name     string
	SortName string
}

// SeriesRef identifies a book's series membership and position.
type SeriesRef struct {
	ID   int64
	Name string
	// Index is the book's position in the series, held as the string the epub
	// carries. EPUB 3.3 Appendix D.3.7 allows "a single xsd:unsignedInt or
	// series of decimal-separated numbers (e.g., 1 or 2.2.1)", and Example 89
	// notes that 98.4 means volume 98, issue 4 — not the number 98.4. A float
	// cannot represent either, and parsing one silently turned every book in a
	// multi-level series into volume 1.
	Index string
}

// DefaultStatus is the reading status assigned to newly ingested books.
const DefaultStatus = "unread"

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta). Location
// and Bib are embedded so their fields read flat (b.Title, b.EpubPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
type Book struct {
	Location
	Bib
	Meta Meta

	// EpubSize is the epub file's size on disk.
	EpubSize int64
}

// NewBook returns a Book with all fields populated.
func NewBook(bib Bib, meta Meta, loc Location) *Book {
	if bib.Authors == nil {
		bib.Authors = []Author{}
	} else {
		bib.Authors = slices.Clone(bib.Authors)
	}
	if bib.Identifiers == nil {
		bib.Identifiers = map[string]string{}
	} else {
		bib.Identifiers = maps.Clone(bib.Identifiers)
	}
	if meta.DateAdded.IsZero() {
		meta.DateAdded = time.Now()
	}
	if meta.DateModified.IsZero() {
		meta.DateModified = time.Now()
	}
	if meta.Status == "" {
		meta.Status = DefaultStatus
	}
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	return &Book{Location: loc, Bib: bib, Meta: meta}
}

// HasSeries reports whether the book belongs to a series.
func (b *Book) HasSeries() bool {
	return b != nil && b.Series != nil
}

// SeriesName returns the book's series name, or "" when the book has no series.
func (b *Book) SeriesName() string {
	if !b.HasSeries() {
		return ""
	}
	return b.Series.Name
}

// ImmutableBook is an immutable snapshot of a book's state. It wraps the
// internal Book and provides read-only access via getters. The wrapper prevents
// external callers from mutating library state, while allowing internal
// construction and mutation within the library package.
//
// A Book is a snapshot at the time it was returned: after an Edit, call
// Library.Content or Search again for updated state.
type ImmutableBook struct {
	inner *Book
}

// NewImmutableBook wraps a Book. The caller must not retain or mutate b after
// passing it to this function.
func NewImmutableBook(b *Book) *ImmutableBook {
	return &ImmutableBook{inner: b}
}

// Unwrap returns the underlying mutable Book. This is for internal use only;
// external code should use the getter methods instead.
func Unwrap(b *ImmutableBook) *Book {
	return b.inner
}

// ID returns the book's unique identifier.
func (b *ImmutableBook) ID() int64 { return b.inner.Meta.ID }

// Title returns the book's title.
func (b *ImmutableBook) Title() string { return b.inner.Title }

// SortTitle returns the book's sort title, or "" if unset.
func (b *ImmutableBook) SortTitle() string { return b.inner.SortTitle }

// Authors returns a copy of the book's authors list.
func (b *ImmutableBook) Authors() []Author { return slices.Clone(b.inner.Authors) }

// Series returns the book's series reference, or nil if the book is not in a series.
func (b *ImmutableBook) Series() *SeriesRef {
	if b.inner.Series == nil {
		return nil
	}
	s := *b.inner.Series
	return &s
}

// HasSeries reports whether the book belongs to a series.
func (b *ImmutableBook) HasSeries() bool { return b.inner.HasSeries() }

// SeriesName returns the book's series name, or "" when the book has no series.
func (b *ImmutableBook) SeriesName() string { return b.inner.SeriesName() }

// SeriesIndex returns the book's position in its series, or "" if not in a series.
func (b *ImmutableBook) SeriesIndex() string {
	if b.inner.Series == nil {
		return ""
	}
	return b.inner.Series.Index
}

// Language returns the book's language code (BCP 47 / ISO 639).
func (b *ImmutableBook) Language() string { return b.inner.Language }

// Pubdate returns the book's publication date as a string.
func (b *ImmutableBook) Pubdate() string { return b.inner.Pubdate }

// Description returns the book's description or summary.
func (b *ImmutableBook) Description() string { return b.inner.Description }

// Identifiers returns a copy of the book's identifiers map (e.g., ISBN, ASIN).
func (b *ImmutableBook) Identifiers() map[string]string { return maps.Clone(b.inner.Identifiers) }

// CoverPath returns the zip-relative path to the cover image, or "" if none.
func (b *ImmutableBook) CoverPath() string { return b.inner.CoverPath }

// OpfSize returns the OPF file's uncompressed size, or 0 if unavailable.
func (b *ImmutableBook) OpfSize() int64 { return b.inner.OpfSize }

// CoverSize returns the cover image's uncompressed size, or 0 if unavailable.
func (b *ImmutableBook) CoverSize() int64 { return b.inner.CoverSize }

// EpubPath returns the book's relative path within the store.
func (b *ImmutableBook) EpubPath() string { return b.inner.EpubPath }

// Dir returns the directory portion of the book's location.
func (b *ImmutableBook) Dir() string { return b.inner.Dir() }

// Filename returns the epub's basename within its directory.
func (b *ImmutableBook) Filename() string { return b.inner.Filename() }

// EpubSize returns the epub file's size on disk.
func (b *ImmutableBook) EpubSize() int64 { return b.inner.EpubSize }

// DateAdded returns when the book was added to the library.
func (b *ImmutableBook) DateAdded() time.Time { return b.inner.Meta.DateAdded }

// DateModified returns when the book's metadata was last modified.
func (b *ImmutableBook) DateModified() time.Time { return b.inner.Meta.DateModified }

// Status returns the book's reading status.
func (b *ImmutableBook) Status() string { return b.inner.Meta.Status }

// Rating returns the book's rating (0-5).
func (b *ImmutableBook) Rating() float64 { return b.inner.Meta.Rating }

// Tags returns a copy of the book's custom tags list.
func (b *ImmutableBook) Tags() []string { return slices.Clone(b.inner.Meta.Tags) }

// Bib holds the bibliographic data parsed from the epub.
type Bib struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *SeriesRef
	Language    string
	Pubdate     string
	Description string
	Identifiers map[string]string
	CoverPath   string // zip-relative path to cover image; empty if none
	OpfSize     int64  // OPF uncompressed size from zip central directory; 0 if unavailable
	CoverSize   int64  // cover uncompressed size from zip central directory; 0 if unavailable
}

// Location identifies where a book lives on disk.
type Location struct {
	EpubPath string
}

// Dir returns the directory portion of the location's relative path.
func (l Location) Dir() string { return filepath.Dir(l.EpubPath) }

// Filename returns the epub's basename within its directory.
func (l Location) Filename() string { return filepath.Base(l.EpubPath) }

// Meta mirrors the meta.toml sidecar schema.
type Meta struct {
	ID           int64     `toml:"id"`
	DateAdded    time.Time `toml:"date_added"`
	DateModified time.Time `toml:"date_modified"`
	Status       string    `toml:"status"` // unread | reading | read | abandoned
	Rating       float64   `toml:"rating"`
	Tags         []string  `toml:"custom_tags"` // toml key preserved for file compatibility
}
