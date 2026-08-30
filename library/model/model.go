// Package model defines the shared types used across store, index, and library.
package model

import (
	"io"
	"strings"
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
