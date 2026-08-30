// Package textfmt parses the textual formats the 9p surface exposes to users —
// the line formats of writable field files and the argument formats of ctl
// commands. They are frontend syntax: the library never reads or writes them,
// so they do not belong in library/model alongside the types they produce.
package textfmt

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/library"
)

// ParseAuthor parses a single author spec in "Name | Sort" form, the format
// used by the authors field file and the ctl rename-author command. The sort
// name is optional; when the "|" or its right side is absent or blank, SortName
// is left empty. Both halves are trimmed. A blank Name (empty or "| Sort") is
// returned as-is for the caller to reject, so this stays a pure parse.
func ParseAuthor(spec string) library.Author {
	name, sortName, _ := strings.Cut(spec, "|")
	return library.Author{Name: strings.TrimSpace(name), SortName: strings.TrimSpace(sortName)}
}
