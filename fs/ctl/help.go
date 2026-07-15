package ctl

import (
	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
)

const helpText = `ctl — control the library

Commands:

  add-tag <tag> <id-spec>
    Add a tag to matching books.

  remove-tag <tag> <id-spec>
    Remove a tag from matching books.

  set-status <status> <id-spec>
    Set reading status for matching books.
    Status: unread, reading, read, abandoned.

  set-rating <rating> <id-spec>
    Set rating (0-5) for matching books.

  delete <id>
    Delete a single book by id.

  reindex
    Rebuild the index from on-disk files.

  rename-tag <old> <new>
    Rename a tag across every book. Renaming <old> onto a
    tag that already exists merges the two: books that had
    only <old> now have <new>; books that had both drop the
    duplicate <old>.

  rename-author <old> <new>
    Rename an author across every book.
    <old> is matched against display name OR sort name.
    <new> uses the "Name | Sort" format (same as the
    authors field file).

  rename-series <old> <new>
    Rename a series across every book.

ID-spec formats:

  *         all books
  42        single book
  1,2,3     comma-separated list

Examples:

  add-tag "science fiction" 1,2,3
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

// NewHelpFile creates a read-only file named "help" that documents the
// available ctl commands.
func NewHelpFile(f *fs.FS) *fs.StaticFile {
	return fs.NewStaticFile(vfile.NewStat(f, "help", 0444), []byte(helpText))
}
