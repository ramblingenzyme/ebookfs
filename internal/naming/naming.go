// Package naming turns arbitrary text into strings safe to use as filesystem
// path components. It is a leaf utility shared by the epub parser and the 9P
// boundary, and depends on nothing else in the tree so neither has to import
// the other to sanitize a name.
package naming

import (
	"errors"
	"strings"
)

// Sanitize makes s safe for use as a filesystem path component.
// It replaces '/' with '-', strips NUL and control characters (< 0x20),
// and trims leading/trailing dots, spaces, and tabs.
// Returns an error if the result is empty.
func Sanitize(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/':
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
