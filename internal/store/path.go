package store

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
)

var ErrEmptyTitle = errors.New("title is empty after sanitization")

// TODO: move this step into the parser?
func sanitize(s string) (string, error) {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '/':
			b.WriteRune('-')
		case r == 0, r < 0x20:
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

func authorDir(name string) (string, error) {
	return sanitize(name)
}

func bookDir(title string, id int64) (string, error) {
	s, err := sanitize(title)
	if err != nil {
		return "", ErrEmptyTitle
	}
	return fmt.Sprintf("%s (%d)", s, id), nil
}

func epubFilenameFromStrings(title, author string) (string, error) {
	t, err := sanitize(title)
	if err != nil {
		return "", ErrEmptyTitle
	}
	if author == "" {
		return fmt.Sprintf("%s.epub", t), nil
	}
	a, err := sanitize(author)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s - %s.epub", t, a), nil
}

func epubFilename(book *epub.Book) (string, error) {
	author := ""
	if len(book.Authors) > 0 {
		author = book.Authors[0].Name
	}
	return epubFilenameFromStrings(book.Title, author)
}

func canonicalPathFromStrings(authorSortName, title string, id int64) (string, error) {
	adir, err := authorDir(authorSortName)
	if err != nil {
		return "", err
	}
	bdir, err := bookDir(title, id)
	if err != nil {
		return "", err
	}
	return filepath.Join(adir, bdir), nil
}

func canonicalPath(book *epub.Book, id int64) (string, error) {
	primaryAuthor := epub.Author{Name: "Unknown"}
	if len(book.Authors) > 0 {
		primaryAuthor = book.Authors[0]
	}

	name := primaryAuthor.SortAs
	if name == "" {
		name = primaryAuthor.Name
	}

	return canonicalPathFromStrings(name, book.Title, id)
}
