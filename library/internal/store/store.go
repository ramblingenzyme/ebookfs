// Package store manages filesystem operations on the library directory tree.
package store

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"syscall"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Store manages filesystem operations on the library directory tree.
type Store struct {
	root      string // absolute path to the library root
	inboxTemp string // absolute path to the inbox temp directory; must be on the same filesystem as root
}

func New(root, inboxTemp string) *Store {
	return &Store{root: root, inboxTemp: inboxTemp}
}

// AbsPath resolves a relative path (the EpubPath stored in a book's Location)
// to an absolute path on disk.
func (s *Store) AbsPath(relPath string) string {
	return filepath.Join(s.root, relPath)
}

// Root returns the absolute path to the library root directory.
func (s *Store) Root() string { return s.root }

// Exists reports whether a book with the given authors and title is already
// in the library, regardless of its database ID. Each book lives in a
// subdirectory named "Title (id)" under the author directory — we iterate
// those subdirectories and check whether any already holds the target epub
// file. Walking avoids glob patterns entirely, so metacharacters like [],
// ?, and * in names are handled correctly.
func (s *Store) Exists(authors []model.Author, title string) bool {
	authorDir := filepath.Join(s.root, authorDirName(authors))
	entries, err := os.ReadDir(authorDir)
	if err != nil {
		return false
	}
	targetEpub := epubFilename(authors, title)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		epubPath := filepath.Join(authorDir, entry.Name(), targetEpub)
		if _, err := os.Stat(epubPath); err == nil {
			return true
		}
	}
	return false
}

// Stat observes the on-disk state of a book's epub and meta.toml — both sizes
// and both modification times — for drift detection. A stat failure is returned
// rather than defaulted away: a zero mtime can never match a real file, so
// recording one would silently force a full reindex on every startup thereafter.
// Callers that need to record a directory they could not observe use
// drift.Unobserved instead.
func (s *Store) Stat(loc model.Location) (drift.PathInfo, error) {
	epubFI, err := os.Stat(s.AbsPath(loc.EpubPath))
	if err != nil {
		return drift.PathInfo{}, err
	}
	metaFI, err := os.Stat(s.metaPath(loc))
	if err != nil {
		return drift.PathInfo{}, err
	}
	return drift.PathInfo{
		Size:      epubFI.Size(),
		EpubMtime: epubFI.ModTime(),
		MetaSize:  metaFI.Size(),
		MetaMtime: metaFI.ModTime(),
	}, nil
}

// Move relocates a book from one location to another, renaming the epub within
// the directory if its filename differs. The caller decides the destination
// (see Layout); the store just performs the move.
func (s *Store) Move(from, to model.Location) error {
	oldPath := filepath.Join(s.root, from.Dir())

	fromDir := from.Dir()
	toDir := to.Dir()
	fromName := from.Filename()
	toName := to.Filename()

	// Same directory, only the epub filename changed (e.g. re-sanitized on a
	// later edit): nothing to move, and the "destination" directory below
	// would just be the book's own current one — skip straight to the
	// in-place rename.
	if toDir == fromDir {
		if toName == fromName {
			return nil
		}
		return os.Rename(filepath.Join(oldPath, fromName), filepath.Join(oldPath, toName))
	}

	apath := filepath.Join(s.root, toDir)
	if _, err := os.Stat(apath); err == nil {
		return fmt.Errorf("destination already exists: %s", toDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(apath), 0755); err != nil {
		return err
	}

	if err := os.Rename(oldPath, apath); err != nil {
		return err
	}

	if toName != fromName {
		if err := os.Rename(filepath.Join(apath, fromName), filepath.Join(apath, toName)); err != nil {
			if rollbackErr := os.Rename(apath, oldPath); rollbackErr != nil {
				slog.Error("rollback failed after epub rename error; directory left in inconsistent state, will be repaired by reindex on next startup",
					"old_path", oldPath, "new_path", apath, "rollback_error", rollbackErr, "original_error", err)
			}
			return err
		}
	}

	return removeIfEmpty(filepath.Dir(oldPath))
}

// removeIfEmpty tries to delete an author directory that may have just lost its
// last book; ENOTEMPTY (other books remain) and ENOENT (already gone) are fine.
func removeIfEmpty(dir string) error {
	if err := os.Remove(dir); err != nil && !errors.Is(err, syscall.ENOTEMPTY) && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Delete removes the book directory at loc from the library.
func (s *Store) Delete(loc model.Location) error {
	path := filepath.Join(s.root, loc.Dir())
	if err := os.RemoveAll(path); err != nil {
		return err
	}

	return removeIfEmpty(filepath.Dir(path))
}
