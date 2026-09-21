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

// ForFAT makes s safe for use as a filename on a FAT filesystem, which an epub
// is copied to directly on a Kobo. FAT forbids \ : * ? " < > | in addition to
// the characters sanitize already handles, and filenames may not end with a
// space or period (covered by the shared trim).
//
// A value that sanitizes away entirely comes back unchanged rather than as an
// error, since every caller wants a name and none of them can do better with
// the failure than use what they were given.
func ForFAT(s string) string {
	out, err := sanitize(s, `/\:*?"<>|`)
	if err != nil {
		return s
	}
	return out
}

// PathSafe makes s usable as a single path component. Metadata values are text
// and are stored as the file wrote them (EPUB 3.3 §5.5.2), so each place that
// turns one into a name, such as a library directory or a 9P entry, makes it
// safe itself.
//
//   - '/' becomes '-', or one component would become two.
//   - leading and trailing dots, spaces and tabs are trimmed, or an author
//     named ".." makes filepath.Join walk out of the library root and a book is
//     written outside it. "." is the same bug one level up.
//
// This cannot fail: a value that trims away entirely becomes "_" rather than an
// error, so callers need no fallback. NamesNothing reports that case ahead of
// time, for a caller that would rather refuse the value than accept the
// placeholder.
func PathSafe(s string) string {
	if out := component(s); out != "" {
		return out
	}
	return "_"
}

// NamesNothing reports whether s trims away entirely, leaving PathSafe nothing
// to work from. Every such value collapses onto the one placeholder, so a
// caller that owns its input, as ebookfs owns a tag, refuses them rather than
// filing them all under the same name.
func NamesNothing(s string) bool { return component(s) == "" }

func component(s string) string {
	return strings.Trim(strings.ReplaceAll(s, "/", "-"), ". \t")
}
