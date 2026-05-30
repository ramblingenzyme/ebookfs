package store

import (
	"fmt"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
)

func EpubFilename(authors []epub.Author, title string) string {
	if len(authors) == 0 {
		return fmt.Sprintf("%s.epub", title)
	}
	return fmt.Sprintf("%s - %s.epub", title, authors[0].Name)
}

func CanonicalPath(authors []epub.Author, title string, id int64) string {
	name := "Unknown"
	if len(authors) > 0 {
		name = authors[0].SortAs
		if name == "" {
			name = authors[0].Name
		}
	}
	return filepath.Join(name, fmt.Sprintf("%s (%d)", title, id))
}
