package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Store manages filesystem operations on the library directory tree.
type Store struct {
	root      string // absolute path to the library root
	inboxTemp string // absolute path to the inbox temp directory; must be on the same filesystem as root
}

func New(root, inboxTemp string) *Store {
	return &Store{root: root, inboxTemp: inboxTemp}
}

func (s *Store) AbsPath(libraryPath, filename string) string {
	return filepath.Join(s.root, libraryPath, filename)
}

// Move renames the book directory to match newAuthor/newTitle, returning the
// new libraryPath and epubFilename.
func (s *Store) Move(b *model.Book, newAuthor epub.Author, newTitle string) (string, string, error) {
	newPath := CanonicalPath([]epub.Author{newAuthor}, newTitle, b.Meta.ID)
	newFilename := EpubFilename([]epub.Author{newAuthor}, newTitle)

	apath := filepath.Join(s.root, newPath)
	if _, err := os.Stat(apath); err == nil {
		return "", "", fmt.Errorf("destination already exists: %s", newPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", "", err
	}

	if err := os.MkdirAll(filepath.Dir(apath), 0755); err != nil {
		return "", "", err
	}

	oldPath := filepath.Join(s.root, b.LibraryPath)
	if err := os.Rename(oldPath, apath); err != nil {
		return "", "", err
	}

	if newFilename != b.EpubFilename {
		if err := os.Rename(filepath.Join(apath, b.EpubFilename), filepath.Join(apath, newFilename)); err != nil {
			_ = os.Rename(apath, oldPath)
			return "", "", err
		}
	}

	return newPath, newFilename, nil
}

// Delete removes the book directory from the library.
func (s *Store) Delete(b *model.Book) error {
	path := filepath.Join(s.root, b.LibraryPath)
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	parent := filepath.Dir(path)
	// Try to delete the author directory; ignore ENOTEMPTY (other books remain) and ENOENT (already gone).
	if err := os.Remove(parent); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	return nil
}
