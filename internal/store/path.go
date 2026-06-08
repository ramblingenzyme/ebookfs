package store

import (
	"fmt"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Layout returns the canonical on-disk location for a book with the given
// authors, title, and id: its directory relative to the library root and the
// epub filename within it. It is the single source of the naming convention;
// ingest and move both lay books down through it.
func Layout(authors []model.Author, title string, id int64) model.Location {
	return model.Location{
		LibraryPath:  canonicalDir(authors, title, id),
		EpubFilename: epubFilename(authors, title),
	}
}

func epubFilename(authors []model.Author, title string) string {
	if len(authors) == 0 {
		return fmt.Sprintf("%s.epub", title)
	}
	return fmt.Sprintf("%s - %s.epub", title, authors[0].Name)
}

func canonicalDir(authors []model.Author, title string, id int64) string {
	name := "Unknown"
	if len(authors) > 0 {
		name = authors[0].SortName
		if name == "" {
			name = authors[0].Name
		}
	}
	return filepath.Join(name, fmt.Sprintf("%s (%d)", title, id))
}
