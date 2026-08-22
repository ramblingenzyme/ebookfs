// Package model defines the shared types used across store, index, and library.
package model

import (
	"io"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// EpubReader provides access to a book's epub content from an open handle.
// The handle keeps the file and zip central directory open so repeated calls
// to OPF or Cover avoid re-reading. Close when done.
//
// Implementations of methods that return an EpubReader (Library.Content,
// Exporter.Open) must return a non-nil reader if err is nil; a nil reader with
// a nil error is a contract violation.
//
// An EpubReader is a snapshot of the book at open time — it does not track
// edits. After a concurrent Edit, call Library.Content again for a handle
// that reads from the updated file.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	OPF() ([]byte, error)   // OPF XML from the open epub
	Cover() ([]byte, error) // cover image from the open epub
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
// and Bib are embedded so their fields read flat (b.Title, b.EpubPath); Meta
// stays named so sidecar state is explicitly addressed as b.Meta.
//
// Book is a plain data record. It does not carry transform methods: applying an
// Edits is not a pure in-memory operation (bib fields are derived from an epub
// re-parse and the location from store.Layout), so Library is the single place
// an updated Book is assembled, as well as the sole orchestrator for persistence
// and transactions. The only methods it carries are nil-safe accessors
// (HasSeries, SeriesName) that read the optional Series pointer, so callers
// don't each re-derive the "no series" check.
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

// HasSeries reports whether the book belongs to a series. It is nil-safe: a nil
// receiver or an Series field both report false.
func (b *Book) HasSeries() bool {
	return b != nil && b.Series != nil
}

// SeriesName returns the book's series name, or "" when the book has no series.
// It is nil-safe.
func (b *Book) SeriesName() string {
	if !b.HasSeries() {
		return ""
	}
	return b.Series.Name
}

// Bib holds the bibliographic data parsed from the epub — the "what the book
// is" half, distinct from the mutable Meta sidecar. It is replaced wholesale
// (re-parse → new Bib) when bib fields are edited through the write path,
// while Location and Meta remain intact.
//
// OpfSize and CoverSize are captured during epub.Parse, from the zip central
// directory, propagated from epub.Parse, and persisted in the index. They
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

// PathSafe makes s usable as a single path component. Metadata values are text
// and are stored as the file wrote them (EPUB 3.3 §5.5.2), so every place that
// turns one into a name — a library directory, a 9P entry — has to make it safe
// itself.
//
// Two rules, and both are load-bearing:
//
//   - '/' becomes '-', or one component would become two.
//   - leading and trailing dots, spaces and tabs are trimmed, or an author
//     named ".." makes filepath.Join walk out of the library root and a book is
//     written outside it. "." is the same bug one level up.
//
// Trimming is what naming.Sanitize did before this moved out of the epub
// parser; dropping it was how the escape got in. Unlike Sanitize this cannot
// fail: a value that trims away entirely becomes "_" rather than an error, so
// callers need no fallback.
func PathSafe(s string) string {
	out := strings.Trim(strings.ReplaceAll(s, "/", "-"), ". \t")
	if out == "" {
		return "_"
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

// Location identifies where a book lives on disk. EpubPath is relative to
// the store root (e.g. "Author/Title (42)/Title - Author.epub"). The Store
// resolves it to an absolute path internally when touching the filesystem;
// every other package uses the fields here without knowing about the root.
type Location struct {
	EpubPath string
}

// Dir returns the directory portion of the location's relative path,
// equivalent to what was LibraryPath before the two were consolidated.
func (l Location) Dir() string { return filepath.Dir(l.EpubPath) }

// Filename returns the epub's basename within its directory,
// equivalent to what was EpubFilename before the two were consolidated.
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

type Stats struct {
	Books        int
	Authors      int
	Series       int
	Tags         int
	TotalSize    int64
	LastAdded    time.Time
	LastModified time.Time
}
