// Package model defines the shared types used across store, index, and library.
package model

import (
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"golang.org/x/text/language"
)

type EpubReader interface {
	io.ReaderAt
	io.Closer
}

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta). Location
// and Bib are embedded so their fields read flat (b.Title, b.LibraryPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
//
// Book may carry self-contained methods (Edit, etc.) that operate on its own
// fields without depending on Store, Index, or the epub parser. Library
// remains the single orchestrator for persistence and transactions; Book methods
// are helpers for in-memory transformation and inspection.
//
// A Book handed across an API boundary is an immutable snapshot: once published
// (returned by the library or stored in a frontend), it must not be modified —
// an update is a fresh Book (see Edit), letting concurrent readers hold the old
// value safely.
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

// Bib holds the bibliographic data parsed from the epub — the "what the book
// is" half, distinct from the mutable Meta sidecar. It is replaced wholesale
// (re-parse → new Bib) when bib fields are edited through the write path,
// while Location and Meta remain intact.
//
// OpfSize, CoverSize, and EpubSize are all captured during epub.Parse (from
// the zip central directory and a single os.Stat), propagated through
// bibFromEpub, and persisted in the index. They let the 9P Stat path report
// file lengths without touching the disk (no zip decompression on directory
// listings).
type Bib struct {
	Title       string
	SortTitle   string
	Authors     []Author
	Series      *SeriesRef
	Language    string
	Pubdate     string
	Description string
	Identifiers map[string]string
	CoverPath   string  // zip-relative path to cover image; empty if none
	OpfSize     int64   // OPF uncompressed size from zip central directory; 0 if unavailable
	CoverSize   int64   // cover uncompressed size from zip central directory; 0 if unavailable
	EpubSize    int64   // on-disk epub file size; 0 if unavailable (pre-v6 index)
}

// UnknownAuthor is the fallback author name used when a book has no author
// metadata. It is injected by ingest and may appear defensively in store path
// and export directory computations.
const UnknownAuthor = "Unknown"

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

// Location identifies where a book lives on disk. EpubSize was historically
// here but moved to Bib so all three file sizes (Opf, Cover, Epub)
// are set together during epub.Parse and flow through bibFromEpub as a unit.
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
	Status       string    `toml:"status"` // unread | reading | read | abandoned
	Rating       float64   `toml:"rating"`
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

// Edits is a partial update to a Book's fields. A nil pointer leaves the field
// untouched; a non-nil pointer (including one to a zero value) applies the
// change. This lets a caller change exactly one field — e.g. just the title —
// without having to supply the rest.
//
// A non-nil Series pointing at "" removes the series. SeriesIndex applied
// without Series ("index-only" edit) updates the position of the book's
// current series, resolved against the live snapshot under the per-book lock.
//
// SortTitle follows the same nil/empty rules. As a special case, changing Title
// without supplying a SortTitle clears any existing sort title — it was derived
// from the old title, so leaving it would make the sort title disagree with the
// title.
type Edits struct {
	// Bib fields (written to the epub OPF).
	Title       *string
	SortTitle   *string
	Description *string
	Language    *string
	Authors     *[]Author
	Series      *string
	SeriesIndex *float64

	// Cover replaces the cover image in the epub. It is applied before any bib
	// edits so that a combined Cover + OPF edit produces a single final epub
	// rewrite. Cover replaces the old Library.WriteCover method: all mutations
	// now flow through Edit, keeping the registry snapshot self-consistent.

	Cover *[]byte

	// Meta fields (written to the meta.toml sidecar).
	Status *string
	Rating *float64
	Tags   *[]string
}

// HasBibEdits reports whether any OPF-level field is non-nil.
func (e Edits) HasBibEdits() bool {
	return e.Title != nil || e.SortTitle != nil || e.Description != nil ||
		e.Language != nil || e.Authors != nil || e.Series != nil || e.SeriesIndex != nil
}

// HasCoverEdit reports whether a cover image replacement is requested.
func (e Edits) HasCoverEdit() bool { return e.Cover != nil }

// ApplyMeta applies the meta fields in e to m. Fields set to nil are left untouched.
func (e Edits) ApplyMeta(m *Meta) {
	if e.Status != nil {
		m.Status = *e.Status
	}
	if e.Rating != nil {
		m.Rating = *e.Rating
	}
	if e.Tags != nil {
		m.Tags = *e.Tags
	}
}

// FieldError pairs a field name with a human-readable validation error message,
// so frontends can display feedback next to the relevant field.
type FieldError struct {
	Field   string
	Message string
}

func (fe FieldError) Error() string { return fe.Field + ": " + fe.Message }

// ValidationError collects per-field validation errors from Validate(). It
// satisfies the error interface for simple "if err != nil" checks; callers can
// use errors.As to recover the structured field map.
type ValidationError []FieldError

func (ve ValidationError) Error() string {
	switch len(ve) {
	case 0:
		return ""
	case 1:
		return ve[0].Error()
	}
	// Multi-field: "field1: msg; field2: msg"
	var s strings.Builder
	for i, fe := range ve {
		if i > 0 {
			s.WriteString("; ")
		}
		s.WriteString(fe.Field)
		s.WriteString(": ")
		s.WriteString(fe.Message)
	}
	return s.String()
}

// Validate validates e against b's current state and returns per-field errors.
// A nil return means all fields are valid.
func (e Edits) Validate(b *Book) *ValidationError {
	var ve ValidationError
	add := func(field, msg string) {
		ve = append(ve, FieldError{Field: field, Message: msg})
	}

	if e.Status != nil {
		switch *e.Status {
		case "unread", "reading", "read", "abandoned":
		default:
			add("status", fmt.Sprintf("invalid status %q: must be unread, reading, read, or abandoned", *e.Status))
		}
	}

	if e.Rating != nil {
		// NaN compares false against both bounds, and once persisted it bricks the
		// index (SQLite binds NaN as NULL, violating the NOT NULL rating column),
		// so it must be rejected here.
		if math.IsNaN(*e.Rating) || *e.Rating < 0 || *e.Rating > 5 {
			add("rating", fmt.Sprintf("invalid rating %g: must be 0-5", *e.Rating))
		}
	}

	if e.Title != nil {
		if strings.TrimSpace(*e.Title) == "" {
			add("title", "title must not be empty")
		}
	}

	if e.Authors != nil {
		if len(*e.Authors) == 0 {
			add("authors", "at least one author is required")
		} else {
			for i, a := range *e.Authors {
				if strings.TrimSpace(a.Name) == "" {
					add("authors", fmt.Sprintf("author %d has an empty name", i+1))
					break
				}
			}
		}
	}

	if e.Tags != nil {
		for i, t := range *e.Tags {
			if strings.TrimSpace(t) == "" {
				add("tags", fmt.Sprintf("tag %d is empty", i+1))
				break
			}
		}
	}

	if e.Language != nil {
		if v := strings.TrimSpace(*e.Language); v != "" {
			if _, err := language.Parse(v); err != nil {
				add("language", fmt.Sprintf("language %q is not a recognised BCP 47 / ISO 639 code", *e.Language))
			}
		}
	}

	if e.Cover != nil {
		if len(*e.Cover) == 0 {
			add("cover", "cover image must not be empty")
		}
		if b.CoverPath == "" {
			add("cover", "book has no cover to replace")
		}
	}

	if e.SeriesIndex != nil {
		if math.IsNaN(*e.SeriesIndex) || math.IsInf(*e.SeriesIndex, 0) {
			add("series_index", fmt.Sprintf("invalid series index %g: must be a finite number", *e.SeriesIndex))
		}
		if e.Series == nil && b.Series == nil {
			add("series_index", "book has no series to set an index on")
		}
	}

	if len(ve) == 0 {
		return nil
	}
	return &ve
}

// Edit returns a shallow copy of b with the meta fields in e applied and
// DateModified set to now. It is a pure in-memory operation; it does not touch
// the epub file or the sidecar. For bib edits the caller must re-parse the epub
// and overwrite b.Bib from the result (see Library.Edit).
func (b *Book) Edit(e Edits) *Book {
	updated := *b
	e.ApplyMeta(&updated.Meta)
	updated.Meta.DateModified = time.Now()
	return &updated
}
