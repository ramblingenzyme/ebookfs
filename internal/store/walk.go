package store

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Entry locates a single book on disk: its directory relative to the library
// root and the epub file within it.
type Entry struct {
	LibraryPath  string
	EpubFilename string
}

// Walk enumerates every book directory in the library. A directory is treated
// as a book if it holds a meta.toml sidecar; the walk does not descend into a
// book directory once found.
func (s *Store) Walk() ([]Entry, error) {
	var entries []Entry
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
		entries = append(entries, Entry{LibraryPath: rel, EpubFilename: epubName})
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
