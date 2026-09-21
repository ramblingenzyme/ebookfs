// Package textfmt parses the textual formats the 9P surface exposes: the line
// formats of writable field files and the argument formats of ctl commands.
// Frontend syntax, which is why it does not live beside the types it produces.
package textfmt

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// ParseAuthor parses an author spec in "Name | Sort" form, used by the authors
// field file and by ctl rename-author. A blank Name comes back as-is for the
// caller to reject, so this stays a pure parse.
func ParseAuthor(spec string) library.Author {
	name, sortName, _ := strings.Cut(spec, "|")
	return library.Author{Name: strings.TrimSpace(name), SortName: strings.TrimSpace(sortName)}
}
