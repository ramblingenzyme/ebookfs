// Package model defines the shared types used across store, index, and library.
package model

import "time"

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta). Location
// and Bib are embedded so their fields read flat (b.Title, b.LibraryPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
type Book struct {
	Location
	Bib
	Meta Meta
}

// Bib holds the bibliographic data parsed from the epub — the "what the book
// is" half, distinct from the mutable Meta sidecar. The ebook-meta edit path
// will eventually replace it wholesale (re-parse → new Bib) while leaving
// Location and Meta intact.
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
