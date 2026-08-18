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

// PathTaken reports whether the library already holds an epub file for these
// authors and this title. Index.Exists is the duplicate rule; this is the
// backstop for what the index cannot see, a book on disk that the indexer
// skipped. Being path-derived it can miss (author order and FAT sanitization
// both change the path), which is why it is a guard and not the rule.
//
// Each book lives in a subdirectory named "Title (id)" under the author
// directory, so we walk those rather than glob — titles may contain [], ? and *.
func (s *Store) PathTaken(authors []model.Author, title string) bool {
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
// Callers that need to record a directory they could not observe store the
// zero drift.PathInfo instead.
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
	if from.EpubPath == to.EpubPath {
		return nil
	}

	fromDir := s.AbsPath(from.Dir())
	toDir := s.AbsPath(to.Dir())

	rollbackFn, err := s.renameDir(fromDir, toDir)
	if err != nil {
		return err
	}

	fromName := filepath.Join(toDir, from.Filename())
	toName := filepath.Join(toDir, to.Filename())
	if toName != fromName {
		if err := os.Rename(fromName, toName); err != nil {
			rollbackFn(err)
			return err
		}
	}

	return removeIfEmpty(filepath.Dir(fromDir))
}

func (s *Store) renameDir(fromDir, toDir string) (func(error), error) {
	if fromDir == toDir {
		return func(_ error) {}, nil
	}

	if _, err := os.Stat(toDir); err == nil {
		return nil, fmt.Errorf("destination already exists: %s", toDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(toDir), 0755); err != nil {
		return nil, err
	}

	if err := os.Rename(fromDir, toDir); err != nil {
		return nil, err
	}

	return func(err error) {
		rollbackErr := os.Rename(toDir, fromDir)
		if rollbackErr != nil {
			slog.Error("rollback failed after epub rename error; directory left in inconsistent state, will be repaired by reindex on next startup",
				"old_path", fromDir, "new_path", toDir, "rollback_error", rollbackErr, "original_error", err)
		}
	}, nil
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

// Update applies the edits to the book's on-disk state: moves it from oldLoc to
// newLoc if necessary, writes the updated meta.toml, and returns the observed
// state for drift detection.
func (s *Store) Update(oldLoc, newLoc model.Location, meta *model.Meta) (drift.PathInfo, error) {
	if err := s.Move(oldLoc, newLoc); err != nil {
		return drift.PathInfo{}, err
	}
	if err := s.writeMeta(newLoc, meta); err != nil {
		return drift.PathInfo{}, err
	}
	return s.Stat(newLoc)
}
