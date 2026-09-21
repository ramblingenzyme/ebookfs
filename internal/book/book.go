// Package book defines the internal book record types used by store, index,
// and library, and Edits, the partial update to one. These types are not
// exposed to external consumers; the public library.Book wrapper provides
// read-only access.
//
// Edits spans the whole record as Book does: seven fields the epub carries,
// plus Status, Rating and Tags, which live in the meta.toml sidecar.
package book

import (
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

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
	// notes that 98.4 means volume 98, issue 4, rather than the number 98.4.
	// A float represents neither, and parsing one collapses every multi-level
	// position to volume 1.
	Index string
}

// UnknownAuthor is the fallback author name used when a book has no author
// metadata. It is injected by ingest and may appear defensively in store path
// and export directory computations.
const UnknownAuthor = "Unknown"

// Reading-status vocabulary. Validate and config's reader.statuses validation
// consult Statuses, so adding a status requires updating both the const block
// and the Statuses slice.
const (
	StatusUnread    = "unread"
	StatusReading   = "reading"
	StatusRead      = "read"
	StatusAbandoned = "abandoned"

	// DefaultStatus is the reading status assigned to newly ingested books.
	DefaultStatus = StatusUnread
)

// Statuses lists every valid reading status, in presentation order.
var Statuses = []string{StatusUnread, StatusReading, StatusRead, StatusAbandoned}

func IsValidStatus(s string) bool {
	return slices.Contains(Statuses, s)
}

// StatusList renders Statuses for error messages: "unread, reading, read, or
// abandoned".
func StatusList() string {
	return strings.Join(Statuses[:len(Statuses)-1], ", ") + ", or " + Statuses[len(Statuses)-1]
}

// AuthorSep joins co-authors in every name derived for filing: the library
// directory, the epub filename, and the reader export's folder. All three must
// agree or a book is written under one name and looked up under another, so
// they read the separator from here rather than spelling it.
const AuthorSep = " & "

// JoinAuthors renders authors as a display string joined by sep, skipping empty
// names and falling back to UnknownAuthor when none remain. Callers differ only
// in sep, AuthorSep for a name being filed and ", " for a log line, so the
// filter and fallback live here rather than being re-derived at each site.
func JoinAuthors(authors []Author, sep string) string {
	names := make([]string, 0, len(authors))
	for _, a := range authors {
		if a.Name != "" {
			names = append(names, a.Name)
		}
	}
	if len(names) == 0 {
		return UnknownAuthor
	}
	return strings.Join(names, sep)
}

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

// HasSeries is safe on a nil Book, so callers resolving a series need no guard.
func (b *Book) HasSeries() bool {
	return b != nil && b.Series != nil
}

func (b *Book) SeriesName() string {
	if !b.HasSeries() {
		return ""
	}
	return b.Series.Name
}

// ImmutableBook wraps a Book in read-only getters, so a caller outside this
// package cannot mutate library state. It is a snapshot; pkg/library.Library
// says when to fetch a fresh one.
type ImmutableBook struct {
	inner *Book
}

// NewImmutableBook wraps a Book. The caller must not retain or mutate b after
// passing it to this function.
func NewImmutableBook(b *Book) *ImmutableBook {
	return &ImmutableBook{inner: b}
}

// Unwrap escapes the read-only wrapper. Only this module's packages may call
// it; everything else goes through the getters.
func Unwrap(b *ImmutableBook) *Book {
	return b.inner
}

func (b *ImmutableBook) ID() int64 { return b.inner.Meta.ID }

func (b *ImmutableBook) Title() string { return b.inner.Title }

func (b *ImmutableBook) SortTitle() string { return b.inner.SortTitle }

func (b *ImmutableBook) Authors() []Author { return slices.Clone(b.inner.Authors) }

func (b *ImmutableBook) Series() *SeriesRef {
	if b.inner.Series == nil {
		return nil
	}
	s := *b.inner.Series
	return &s
}

func (b *ImmutableBook) HasSeries() bool { return b.inner.HasSeries() }

func (b *ImmutableBook) SeriesName() string { return b.inner.SeriesName() }

func (b *ImmutableBook) SeriesIndex() string {
	if b.inner.Series == nil {
		return ""
	}
	return b.inner.Series.Index
}

// Language is a BCP 47 / ISO 639 code.
func (b *ImmutableBook) Language() string { return b.inner.Language }

func (b *ImmutableBook) Pubdate() string { return b.inner.Pubdate }

func (b *ImmutableBook) Description() string { return b.inner.Description }

func (b *ImmutableBook) Identifiers() map[string]string { return maps.Clone(b.inner.Identifiers) }

func (b *ImmutableBook) CoverPath() string { return b.inner.CoverPath }

func (b *ImmutableBook) OpfSize() int64 { return b.inner.OpfSize }

func (b *ImmutableBook) CoverSize() int64 { return b.inner.CoverSize }

func (b *ImmutableBook) EpubPath() string { return b.inner.EpubPath }

func (b *ImmutableBook) Dir() string { return b.inner.Dir() }

func (b *ImmutableBook) Filename() string { return b.inner.Filename() }

func (b *ImmutableBook) EpubSize() int64 { return b.inner.EpubSize }

func (b *ImmutableBook) DateAdded() time.Time { return b.inner.Meta.DateAdded }

func (b *ImmutableBook) DateModified() time.Time { return b.inner.Meta.DateModified }

func (b *ImmutableBook) Status() string { return b.inner.Meta.Status }

func (b *ImmutableBook) Rating() float64 { return b.inner.Meta.Rating }

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
	EpubPath string // relative to the store root
}

func (l Location) Dir() string { return filepath.Dir(l.EpubPath) }

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
