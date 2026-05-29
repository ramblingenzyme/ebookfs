package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
)

// Library implements Store against a real directory tree.
type Library struct {
	root      string // absolute path to the library root
	inboxTemp string // absolute path to the inbox temp directory; must be on the same filesystem as root
}

func New(root, inboxTemp string) *Library {
	return &Library{root: root, inboxTemp: inboxTemp}
}

func (l *Library) absPath(b *StoredBook, filename string) string {
	return filepath.Join(l.root, b.LibraryPath, filename)
}

// Move renames the book directory to match newAuthor/newTitle.
func (l *Library) Move(b *StoredBook, newAuthor epub.Author, newTitle string) (*StoredBook, error) {
	sortName := newAuthor.SortAs
	if sortName == "" {
		sortName = newAuthor.Name
	}
	cpath, err := canonicalPathFromStrings(sortName, newTitle, b.ID)
	if err != nil {
		return nil, err
	}
	filename, err := epubFilenameFromStrings(newTitle, newAuthor.Name)
	if err != nil {
		return nil, err
	}

	apath := filepath.Join(l.root, cpath)
	if _, err := os.Stat(apath); err == nil {
		return nil, fmt.Errorf("destination already exists: %s", cpath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(apath), 0755); err != nil {
		return nil, err
	}

	oldPath := filepath.Join(l.root, b.LibraryPath)
	if err := os.Rename(oldPath, apath); err != nil {
		return nil, err
	}

	if filename != b.EpubFilename {
		if err := os.Rename(filepath.Join(apath, b.EpubFilename), filepath.Join(apath, filename)); err != nil {
			_ = os.Rename(apath, oldPath)
			return nil, err
		}
	}

	return &StoredBook{
		ID:           b.ID,
		LibraryPath:  cpath,
		EpubFilename: filename,
	}, nil
}

// Delete removes the book directory from the library.
func (l *Library) Delete(b *StoredBook) error {
	// 1. os.RemoveAll the absolute path to b's directory.
	path := filepath.Join(l.root, b.LibraryPath)
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
