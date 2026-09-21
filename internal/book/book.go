// Package book defines the internal book record types used by store, index,
// and library, and Edits, the partial update to one.
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

const UnknownAuthor = "Unknown"

// Referenced in Validate, reader.statuses & Statuses, so adding a new status requires updating all.
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

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta).
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

func (b *Book) HasSeries() bool {
	return b != nil && b.Series != nil
}

func (b *Book) SeriesName() string {
	if !b.HasSeries() {
		return ""
	}
	return b.Series.Name
}
