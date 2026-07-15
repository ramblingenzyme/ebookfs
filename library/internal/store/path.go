package store

import (
	"fmt"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/library/internal/naming"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Layout returns the canonical on-disk location for a book with the given
// authors, title, and id: its directory relative to the library root and the
// epub filename within it. It is the single source of the naming convention;
// ingest and move both lay books down through it.
func (s *Store) Layout(authors []model.Author, title string, id int64) model.Location {
	libPath := canonicalDir(authors, title, id)
	filename := epubFilename(authors, title)
	return model.Location{
		LibraryPath:  libPath,
		EpubFilename: filename,
		EpubPath:     filepath.Join(s.root, libPath, filename),
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
	joined := model.JoinAuthors(authors, " & ")
	fatAuthor, err := naming.ForFAT(joined)
	if err != nil {
		fatAuthor = joined
	}
	return fmt.Sprintf("%s - %s.epub", fatTitle, fatAuthor)
}

func authorDirName(authors []model.Author) string {
	return model.JoinAuthors(authors, " & ")
}

func canonicalDir(authors []model.Author, title string, id int64) string {
	return filepath.Join(authorDirName(authors), fmt.Sprintf("%s (%d)", title, id))
}
