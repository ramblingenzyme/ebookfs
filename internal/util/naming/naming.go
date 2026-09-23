// Package naming turns arbitrary text into strings safe to use as filesystem
// path components.
package naming

import "strings"

// fatComponent and pathComponent are the two character rules, one per
// destination. Each returns "" when nothing survives, leaving the fallback to
// its caller.
//
// FAT forbids \ : * ? " < > | and every control character, so fatComponent
// drops all of them. POSIX and 9P bar only '/' and NUL, so pathComponent drops
// no more than that: a name is metadata stored as the file wrote it (EPUB 3.3
// §5.5.2), and removing a character the destination accepts loses part of it.
//
// Both trim leading and trailing dots, spaces and tabs. FAT allows no trailing
// space or period, and a leading ".." lets filepath.Join walk out of the
// library root.
const (
	fatForbidden  = `/\:*?"<>|`
	pathForbidden = "/"
	trimmed       = ". \t"
)

func fatComponent(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case strings.ContainsRune(fatForbidden, r):
			b.WriteRune('-')
		case r < 0x20:
			// control characters, NUL among them
		default:
			b.WriteRune(r)
		}
	}
	return strings.Trim(b.String(), trimmed)
}

func pathComponent(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	return strings.Trim(strings.ReplaceAll(s, pathForbidden, "-"), trimmed)
}

// ForFAT makes s safe for use as a filename on a FAT filesystem, which an epub
// is copied to directly on a Kobo.
//
// A value that sanitizes away entirely comes back unchanged rather than as an
// error, since every caller wants a name and none of them can do better with
// the failure than use what they were given.
func ForFAT(s string) string {
	if out := fatComponent(s); out != "" {
		return out
	}
	return s
}

// PathSafe makes s usable as a single path component, for a library directory
// or a 9P entry. It cannot fail: a value that trims away entirely becomes "_"
// rather than an error, so callers need no fallback. NamesNothing reports that
// case ahead of time, for a caller that would rather refuse the value.
func PathSafe(s string) string {
	if out := pathComponent(s); out != "" {
		return out
	}
	return "_"
}

// NamesNothing reports whether s trims away entirely, leaving PathSafe nothing
// to work from. Every such value collapses onto the one placeholder, so a
// caller that owns its input, as ebookfs owns a tag, refuses them rather than
// filing them all under the same name.
func NamesNothing(s string) bool { return pathComponent(s) == "" }
