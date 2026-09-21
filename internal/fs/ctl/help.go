package ctl

import (
	"fmt"
	"strings"

	"github.com/knusbaum/go9p/fs"
)

// The help file's fixed halves. The command entries between them are rendered
// from the commands table, so a verb documented here but never wired, or wired
// and never documented, is not a state this file can reach.
const (
	helpHeader = `ctl — control the library

Commands:

`
	helpFooter = `ID-spec formats:

  *         all books
  42        single book
  1,2,3     comma-separated list
  <query>   same syntax as the search view (see below)

Query syntax:

  prefix:value joined by "+". Prefixes: author, tag,
  series, status, id, title. Repeating a prefix ORs the
  values; different prefixes AND together.

    tag:sci-fi+tag:fantasy    either tag
    tag:sci-fi+status:unread  the tag AND the status

  A value containing spaces must be quoted, since the
  command line splits on unquoted whitespace:
  author:"Isaac Asimov". Quoting the whole spec works too.

  Every prefix is an exact match here, title: included —
  in the search view title: matches substrings, but a
  selection for a mutating command does not.

  A query selects every matching book, so it can be as
  sweeping as "*" without looking like it.

Examples:

  add-tag "science fiction" 1,2,3
  add-tag classic author:"Isaac Asimov"+status:read
  set-status reading *
  rename-author "Asimov" "Isaac Asimov|Asimov, Isaac"
  rename-tag "scifi" "sci-fi"

Notes:

  • Operations continue on error; see the log file for details.
  • rename-tag doubles as a merge: renaming a tag onto an
    existing one folds the two together (the old tag is
    dropped from books that already had the new one).
  • rename-author matches authors whose name OR sort-name
    equals <old>, then replaces both with the new value.
`
)

// helpText renders the help file: each command's signature and description
// indented under the header, then the footer.
func helpText() string {
	var b strings.Builder
	b.WriteString(helpHeader)
	for _, c := range commands {
		fmt.Fprintf(&b, "  %s %s\n", c.name, c.params)
		for line := range strings.SplitSeq(c.desc, "\n") {
			fmt.Fprintf(&b, "    %s\n", line)
		}
		b.WriteString("\n")
	}
	b.WriteString(helpFooter)
	return b.String()
}

// NewHelpFile creates a read-only file named "help" that documents the
// available ctl commands.
func NewHelpFile(f *fs.FS) *fs.StaticFile {
	return fs.NewStaticFile(newStat(f, "help", 0444), []byte(helpText()))
}
