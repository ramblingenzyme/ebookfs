// Package model defines the shared types used across store, index, and library.
package model

import "time"

// Book is the complete record for a book in the library, combining filesystem
// location, bibliographic metadata, and sidecar fields.
type Book struct {
	Meta         Meta
	Title        string
	SortTitle    string
	Authors      []Author
	Series       *SeriesRef // nil if the book has no series
	Language     string
	Pubdate      string            // ISO 8601, may be partial
	Description  string
	Identifiers  map[string]string // scheme → value, e.g. "isbn" → "978-..."
	LibraryPath  string            // relative to library root
	EpubFilename string
	HasCover     bool
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

// Meta mirrors the meta.toml sidecar schema.
type Meta struct {
	ID           int64     `toml:"id"`
	DateAdded    time.Time `toml:"date_added"`
	DateModified time.Time `toml:"date_modified"`
	Status       string    `toml:"status"`      // unread | reading | read | abandoned
	Rating       int       `toml:"rating"`      // 0–5, 0 = unrated
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
