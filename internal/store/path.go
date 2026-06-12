package store

import (
	"fmt"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/model"
	"github.com/ramblingenzyme/ebookfs/internal/naming"
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
	fatAuthor, err := naming.ForFAT(authors[0].Name)
	if err != nil {
		fatAuthor = authors[0].Name
	}
	return fmt.Sprintf("%s - %s.epub", fatTitle, fatAuthor)
}

func authorDirName(authors []model.Author) string {
	if len(authors) == 0 {
		return "Unknown"
	}
	name := authors[0].SortName
	if name == "" {
		name = authors[0].Name
	}
	return name
}

func canonicalDir(authors []model.Author, title string, id int64) string {
	return filepath.Join(authorDirName(authors), fmt.Sprintf("%s (%d)", title, id))
}
