// Package model defines the shared types used across store, index, and library.
package model

import (
	"os"
	"time"
)

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta). Location
// and Bib are embedded so their fields read flat (b.Title, b.LibraryPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
//
// Book may carry self-contained methods (Stat, WithBib, etc.) that operate on
// its own fields without depending on Store, Index, or the epub parser. Library
// remains the single orchestrator for persistence and transactions; Book methods
// are helpers for in-memory transformation and inspection.
type Book struct {
	Location
	Bib
	Meta Meta
}

// NewBook returns a Book with all fields populated. Location is required so
// EpubPath is always set; the caller computes it via store.Layout or store.Walk.
// Zero-valued fields are set to sensible defaults so callers don't need to
// repeat them.
func NewBook(bib Bib, meta Meta, loc Location) *Book {
	if bib.Authors == nil {
		bib.Authors = []Author{}
	}
	if bib.Identifiers == nil {
		bib.Identifiers = map[string]string{}
	}
	if meta.DateAdded.IsZero() {
		meta.DateAdded = time.Now()
	}
	if meta.DateModified.IsZero() {
		meta.DateModified = time.Now()
	}
	if meta.Status == "" {
		meta.Status = "unread"
	}
	if meta.Tags == nil {
		meta.Tags = []string{}
	}
	return &Book{Location: loc, Bib: bib, Meta: meta}
}

// Stat returns the FileInfo for the book's epub file. EpubPath must be
// populated, which it always is when NewBook created the Book.
func (b *Book) Stat() (os.FileInfo, error) {
	return os.Stat(b.EpubPath)
}

// Bib holds the bibliographic data parsed from the epub — the "what the book
// is" half, distinct from the mutable Meta sidecar. It is replaced wholesale
// (re-parse → new Bib) when bib fields are edited through the write path,
// while Location and Meta remain intact.
type Bib struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *SeriesRef // nil if the book has no series
	Language    string
	Pubdate     string // ISO 8601, may be partial
	Description string
	Identifiers map[string]string // scheme → value, e.g. "isbn" → "978-..."
	CoverPath   string            // zip-relative path to cover image; empty if none
}

type Author struct {
	ID       int64
	Name     string
	SortName string
}

type SeriesRef struct {
	ID    int64
	Name  string
	Index float64
}

type Location struct {
	LibraryPath  string
	EpubFilename string
	EpubPath     string // absolute path to the epub file; set when Location is created
}

// Meta mirrors the meta.toml sidecar schema.
type Meta struct {
	ID           int64     `toml:"id"`
	DateAdded    time.Time `toml:"date_added"`
	DateModified time.Time `toml:"date_modified"`
	Status       string    `toml:"status"`      // unread | reading | read | abandoned
	Rating       int       `toml:"rating"`      // TODO: should be float32 0–5 (e.g. 4.75 is valid); int is a placeholder
	Tags         []string  `toml:"custom_tags"` // toml key preserved for file compatibility
}

type Stats struct {
	Books        int
	Authors      int
	Series       int
	Tags         int
	TotalSize    int64
	LastAdded    time.Time
	LastModified time.Time
}
