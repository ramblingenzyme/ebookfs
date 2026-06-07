package store

import (
	"fmt"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

func EpubFilename(authors []model.Author, title string) string {
	if len(authors) == 0 {
		return fmt.Sprintf("%s.epub", title)
	}
	return fmt.Sprintf("%s - %s.epub", title, authors[0].Name)
}

func CanonicalPath(authors []model.Author, title string, id int64) string {
	name := "Unknown"
	if len(authors) > 0 {
		name = authors[0].SortName
		if name == "" {
			name = authors[0].Name
		}
	}
	return filepath.Join(name, fmt.Sprintf("%s (%d)", title, id))
}
