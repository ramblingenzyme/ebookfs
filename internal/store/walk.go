package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Walk enumerates every book directory in the library, returning each book's
// location. A directory is treated as a book if it holds a meta.toml sidecar;
// the walk does not descend into a book directory once found.
func (s *Store) Walk() ([]model.Location, error) {
	var entries []model.Location
	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, "meta.toml")); err != nil {
			return nil // not a book directory; keep descending
		}

		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}
		epubName, err := findEpub(path)
		if err != nil {
			return err
		}
		entries = append(entries, model.Location{LibraryPath: rel, EpubFilename: epubName})
		return filepath.SkipDir // a book directory has no nested books
	})
	return entries, err
}

func findEpub(dir string) (string, error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	for _, e := range ents {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".epub") {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("no epub in %s", dir)
}
