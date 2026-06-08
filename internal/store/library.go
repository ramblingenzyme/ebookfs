package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

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

// OpenEpub opens the epub file at loc for reading. The caller closes it.
func (s *Store) OpenEpub(loc model.Location) (*os.File, error) {
	return os.Open(s.AbsPath(loc.LibraryPath, loc.EpubFilename))
}

// Move relocates a book from one location to another, renaming the epub within
// the directory if its filename differs. The caller decides the destination
// (see Layout); the store just performs the move.
func (s *Store) Move(from, to model.Location) error {
	apath := filepath.Join(s.root, to.LibraryPath)
	if _, err := os.Stat(apath); err == nil {
		return fmt.Errorf("destination already exists: %s", to.LibraryPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(apath), 0755); err != nil {
		return err
	}

	oldPath := filepath.Join(s.root, from.LibraryPath)
	if err := os.Rename(oldPath, apath); err != nil {
		return err
	}

	if to.EpubFilename != from.EpubFilename {
		if err := os.Rename(filepath.Join(apath, from.EpubFilename), filepath.Join(apath, to.EpubFilename)); err != nil {
			_ = os.Rename(apath, oldPath)
			return err
		}
	}

	return nil
}

// Delete removes the book directory at loc from the library.
func (s *Store) Delete(loc model.Location) error {
	path := filepath.Join(s.root, loc.LibraryPath)
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
