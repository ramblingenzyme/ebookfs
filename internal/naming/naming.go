// Package naming turns arbitrary text into strings safe to use as filesystem
// path components. It is a leaf utility shared by the epub parser and the 9P
// boundary, and depends on nothing else in the tree so neither has to import
// the other to sanitize a name.
package naming

import (
	"errors"
	"strings"
)

// sanitize replaces the runes of forbidden with '-', strips NUL and control
// characters (< 0x20), and trims leading/trailing dots, spaces, and tabs.
// Returns an error if the result is empty.
func sanitize(s, forbidden string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(forbidden, r):
			b.WriteRune('-')
		case r < 0x20:
			// strip NUL and control characters
		default:
			b.WriteRune(r)
		}
	}
	out := strings.Trim(b.String(), ". \t")
	if out == "" {
		return "", errors.New("sanitized string is empty")
	}
	return out, nil
}

// ForFAT makes s safe for use as a filename on a FAT filesystem.
// FAT forbids \ : * ? " < > | in addition to the characters Sanitize already
// handles, and filenames may not end with a space or period (covered by the
// shared trim).
func ForFAT(s string) (string, error) {
	return sanitize(s, `/\:*?"<>|`)
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
