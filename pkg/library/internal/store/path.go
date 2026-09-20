package store

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
)

// Layout returns the canonical on-disk location for a book with the given
// authors, title, and id: its path relative to the library root (EpubPath) is
// set; the Store resolves it to an absolute path internally when touching the
// filesystem. It is the single source of the naming convention; ingest and move
// both lay books down through it.
func (s *Store) Layout(authors []book.Author, title string, id int64) book.Location {
	libPath := canonicalDir(authors, title, id)
	filename := epubFilename(authors, title)
	return book.Location{
		EpubPath: filepath.Join(libPath, filename),
	}
}

func epubFilename(authors []book.Author, title string) string {
	// Epub files may be copied directly to FAT filesystems (e.g. Kobo), so
	// sanitize components for FAT; fall back to the raw value if sanitization
	// produces an empty string (pathological titles/authors).
	fatTitle, err := naming.ForFAT(title)
	if err != nil {
		fatTitle = title
	}
	if len(authors) == 0 {
		return fmt.Sprintf("%s.epub", fatTitle)
	}
	joined := book.JoinAuthors(authors, " & ")
	fatAuthor, err := naming.ForFAT(joined)
	if err != nil {
		fatAuthor = joined
	}
	return fmt.Sprintf("%s - %s.epub", fatTitle, fatAuthor)
}

// Both components are made path-safe: a '/' in a title or an author name would
// otherwise split one directory into two. epubFilename does its own, stricter
// pass for the file itself.
func authorDirName(authors []book.Author) string {
	return naming.PathSafe(book.JoinAuthors(authors, " & "))
}

func canonicalDir(authors []book.Author, title string, id int64) string {
	return filepath.Join(authorDirName(authors), fmt.Sprintf("%s (%d)", naming.PathSafe(title), id))
}

// IDFromPath recovers the book id that canonicalDir encoded in a library path's
// trailing " (id)". Every layout this project has used — canonical, legacy
// single-author, legacy sort-name — shares that suffix, so it is readable even
// for a book whose meta.toml can't be parsed.
//
// A best-effort reading, reported as absent rather than as an error: a
// hand-made directory need not carry an id, and the caller only uses it to
// avoid reissuing an id that may still be in use.
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
