// Package textfmt parses the textual formats the 9P surface exposes: the line
// formats of writable field files and the argument formats of ctl commands.
// Frontend syntax, which is why it does not live beside the types it produces.
package textfmt

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ParsePair splits one "First | Second" line of the authors and contributors
// field files, trimming both halves. The second half is "" when the line has
// no "|".
func ParsePair(line string) (first, second string) {
	first, second, _ = strings.Cut(line, "|")
	return strings.TrimSpace(first), strings.TrimSpace(second)
}

// FormatPair is the inverse of ParsePair, omitting the separator when second
// is "".
func FormatPair(first, second string) string {
	if second == "" {
		return first
	}
	return first + " | " + second
}

// ParseAuthor parses an author spec in "Name | Sort" form, used by the authors
// field file and by ctl rename-author. A blank Name comes back as-is for the
// caller to reject, so this stays a pure parse.
func ParseAuthor(spec string) library.Author {
	name, sortName := ParsePair(spec)
	return library.Author{Name: name, SortName: sortName}
}
