// Package model defines the shared types used across store, index, and library.
package model

import (
	"io"
	"slices"
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
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

// Reading-status vocabulary. This package defines the valid set; edits.Validate
// and config's reader.statuses validation consult Statuses, so adding a status
// requires updating both the const block and the Statuses slice.
const (
	StatusUnread    = "unread"
	StatusReading   = "reading"
	StatusRead      = "read"
	StatusAbandoned = "abandoned"

	// DefaultStatus is the status a freshly ingested book carries.
	DefaultStatus = book.DefaultStatus
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
// This cannot fail: a value that trims away entirely becomes "_" rather than an
// error, so callers need no fallback.
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

// Author represents a book author with optional sort name for display.
type Author = book.Author

// SeriesRef identifies a book's series membership and position.
type SeriesRef = book.SeriesRef

type Stats struct {
	Books        int
	Authors      int
	Series       int
	Tags         int
	TotalSize    int64
	LastAdded    time.Time
	LastModified time.Time
}
