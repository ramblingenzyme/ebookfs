// Package model defines the shared types used across store, index, and library.
package model

import (
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"time"

	"golang.org/x/text/language"
)

// EpubReader provides access to a book's epub content from an open handle.
// The handle keeps the file and zip central directory open so repeated calls
// to OPF or Cover avoid re-reading. Close when done.
//
// An EpubReader is a snapshot of the book at open time — it does not track
// edits. After a concurrent Edit, call Library.Content again for a handle
// that reads from the updated file.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	OPF() ([]byte, error)     // OPF XML from the open epub
	Cover() ([]byte, error)   // cover image from the open epub
}

// Reading-status vocabulary. This package owns the set: Edits.Validate, the
// NewBook default, and config's reader.statuses validation all consult it, so
// adding a status is a one-line change here.
const (
	StatusUnread    = "unread"
	StatusReading   = "reading"
	StatusRead      = "read"
	StatusAbandoned = "abandoned"

	// DefaultStatus is the status a freshly ingested book carries.
	DefaultStatus = StatusUnread
)

// Statuses lists every valid reading status, in presentation order.
var Statuses = []string{StatusUnread, StatusReading, StatusRead, StatusAbandoned}

// IsValidStatus reports whether s is one of Statuses.
func IsValidStatus(s string) bool {
	return slices.Contains(Statuses, s)
}

// StatusList renders Statuses for error messages: "unread, reading, read, or
// abandoned".
func StatusList() string {
	return strings.Join(Statuses[:len(Statuses)-1], ", ") + ", or " + Statuses[len(Statuses)-1]
}

// Book is the complete record for a book in the library: where it lives
// (Location), what it is (Bib), and its mutable sidecar state (Meta). Location
// and Bib are embedded so their fields read flat (b.Title, b.LibraryPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
//
// Book is a plain data record. It does not carry transform methods: applying an
// Edits is not a pure in-memory operation (bib fields are derived from an epub
// re-parse and the location from store.Layout), so Library is the single place
// an updated Book is assembled, as well as the sole orchestrator for persistence
// and transactions.
//
// A Book handed across an API boundary is an immutable snapshot: once published
// (returned by the library or stored in a frontend), it must not be modified —
// an update is a fresh Book (see Edit), letting concurrent readers hold the old
// value safely.
type Book struct {
	Location
	Bib
	Meta Meta

	// EpubSize is the epub file's size on disk. It sits outside Location and Bib
	// because it is observed rather than derived: the library stats the epub
	// before every index write and fails the write if it cannot, so the size the
	// index stores is the same observation the drift check compares against, and
	// there is no "unknown" case for readers to guard. Both Location (by a move)
	// and Bib (by a re-parse) are replaced wholesale during an edit, which would
	// silently discard it from either. Set by bookFromBib in the library package
	// for ingest and reindex, and directly in Edit after the stat.
	EpubSize int64
}

// NewBook returns a Book with all fields populated. Location is required so
// EpubPath is always set; the caller computes it via store.Layout or store.Walk.
// Zero-valued fields are set to sensible defaults so callers don't need to
// repeat them.
func NewBook(bib Bib, meta Meta, loc Location) *Book {
	if bib.Authors == nil {
		bib.Authors = []Author{}
	} else {
		bib.Authors = slices.Clone(bib.Authors)
	}
	if bib.Identifiers == nil {
		bib.Identifiers = map[string]string{}
	} else {
		bib.Identifiers = cloneMap(bib.Identifiers)
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

// Bib holds the bibliographic data parsed from the epub — the "what the book
// is" half, distinct from the mutable Meta sidecar. It is replaced wholesale
// (re-parse → new Bib) when bib fields are edited through the write path,
// while Location and Meta remain intact.
//
// OpfSize and CoverSize are captured during epub.Parse, from the zip central
// directory, propagated through bibFromEpub, and persisted in the index. They
// let the 9P Stat path report file lengths without touching the disk (no zip
// decompression on directory listings). The epub's own size is not among them —
// it describes the file rather than its contents, so it lives on Book.
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

// UnknownAuthor is the fallback author name used when a book has no author
// metadata. It is injected by ingest and may appear defensively in store path
// and export directory computations.
const UnknownAuthor = "Unknown"

// cloneMap returns a shallow copy of m. Helper for NewBook.
func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	if m == nil {
		return nil
	}
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// JoinAuthors renders authors as a display string joined by sep, skipping empty
// names and falling back to UnknownAuthor when none remain. Callers differ only
// in sep (" & " for directory names, ", " for log lines), so the filter and
// fallback live here rather than being re-derived at each site.
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

type Author struct {
	ID       int64
	Name     string
	SortName string
}

// ParseAuthor parses a single author spec in "Name | Sort" form, the format
// used by the authors field file and the ctl rename-author command. The sort
// name is optional; when the "|" or its right side is absent or blank, SortName
// is left empty. Both halves are trimmed. A blank Name (empty or "| Sort") is
// returned as-is for the caller to reject, so this stays a pure parse.
func ParseAuthor(spec string) Author {
	name, sortName, _ := strings.Cut(spec, "|")
	return Author{Name: strings.TrimSpace(name), SortName: strings.TrimSpace(sortName)}
}

type SeriesRef struct {
	ID    int64
	Name  string
	Index float64
}

// Location identifies where a book lives on disk.
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

// Normalized returns a copy of e with the persisted-precision rules applied:
// ratings are stored to 2 decimal places and series indices to 1. Rounding is
// a storage policy enforced by Library.Edit for every caller, not a parsing
// concern re-implemented by each frontend. NaN/Inf survive rounding and are
// rejected by Validate.
func (e Edits) Normalized() Edits {
	if e.Rating != nil {
		r := math.Round(*e.Rating*100) / 100
		e.Rating = &r
	}
	if e.SeriesIndex != nil {
		i := math.Round(*e.SeriesIndex*10) / 10
		e.SeriesIndex = &i
	}
	return e
}

// HasBibEdits reports whether any OPF-level field is non-nil.
func (e Edits) HasBibEdits() bool {
	return e.Title != nil || e.SortTitle != nil || e.Description != nil ||
		e.Language != nil || e.Authors != nil || e.Series != nil || e.SeriesIndex != nil
}

// HasCoverEdit reports whether a cover image replacement is requested.
func (e Edits) HasCoverEdit() bool { return e.Cover != nil }

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

	if e.Status != nil && !IsValidStatus(*e.Status) {
		add("status", fmt.Sprintf("invalid status %q: must be %s", *e.Status, StatusList()))
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
