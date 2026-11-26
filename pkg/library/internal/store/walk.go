package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Walk treats a directory as a book if it holds a meta.toml sidecar.
func (s *Store) Walk() ([]book.File, error) {
	var entries []book.File

	err := filepath.WalkDir(s.root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if !d.IsDir() {
			return nil
		}

		if path == filepath.Join(s.root, trashDirName) {
			return filepath.SkipDir // deleted books; reindexing them would undo the delete
		}

		if _, err := os.Stat(filepath.Join(path, metaFilename)); errors.Is(err, fs.ErrNotExist) {
			return nil // not a book directory; keep descending
		} else if err != nil {
			return err
		}

		rel, err := filepath.Rel(s.root, path)
		if err != nil {
			return err
		}

		name, format, err := findFile(path)
		if err != nil {
			return filepath.SkipDir // directory has meta.toml but no book file; skip it rather than aborting the walk
		}

		entries = append(entries, book.File{
			Path:   filepath.Join(rel, name),
			Format: format,
		})

		return filepath.SkipDir // a book directory has no nested books
	})

	return entries, err
}

func findFile(dir string) (name string, format book.Format, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return "", "", err
	}

	for _, e := range ents {
		if e.IsDir() {
			continue
		}

		if f, ok := book.FormatFromExtension(filepath.Ext(e.Name())); ok {
			return e.Name(), f, nil
		}
	}

	return "", "", fmt.Errorf("no book file in %s", dir)
}
