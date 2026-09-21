package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
)

// Layout is the single source of the naming convention, and ingest and move
// both lay books down through it. Only EpubPath is set, relative to the
// library root; the Store resolves it when it touches the filesystem.
func (s *Store) Layout(authors []book.Author, title string, id int64) book.Location {
	libPath := canonicalDir(authors, title, id)
	filename := epubFilename(authors, title)
	return book.Location{
		EpubPath: filepath.Join(libPath, filename),
	}
}

func epubFilename(authors []book.Author, title string) string {
	fatTitle := naming.ForFAT(title)
	if len(authors) == 0 {
		return fmt.Sprintf("%s.epub", fatTitle)
	}
	return fmt.Sprintf("%s - %s.epub", fatTitle, naming.ForFAT(book.JoinAuthors(authors, book.AuthorSep)))
}

func authorDirName(authors []book.Author) string {
	return naming.PathSafe(book.JoinAuthors(authors, book.AuthorSep))
}

// Both components are made path-safe, since a '/' in a title or an author name
// would otherwise split one directory into two. epubFilename does its own,
// stricter pass for the file itself.
func canonicalDir(authors []book.Author, title string, id int64) string {
	return filepath.Join(authorDirName(authors), fmt.Sprintf("%s (%d)", naming.PathSafe(title), id))
}

// IDFromPath reads the " (id)" suffix canonicalDir writes. Every layout this
// project has used shares that suffix, so an id is recoverable even from a book
// whose meta.toml will not parse.
//
// Absence is not an error. A hand-made directory need not carry an id, and the
// caller only uses one to avoid reissuing an id that may still be in use.
func IDFromPath(libraryPath string) (int64, bool) {
	base := filepath.Base(libraryPath)
	if !strings.HasSuffix(base, ")") {
		return 0, false
	}
	open := strings.LastIndex(base, " (")
	if open < 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(base[open+2:len(base)-1], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
