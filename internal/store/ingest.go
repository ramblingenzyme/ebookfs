package store

import (
	"log"
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// Ingest materializes b on disk: it creates the book directory at b.LibraryPath,
// moves the staged epub into place as b.EpubFilename, and writes the meta.toml
// sidecar from b.Meta. b.LibraryPath and b.EpubFilename must already be set by
// the caller (see CanonicalPath and EpubFilename).
func (s *Store) Ingest(b *model.Book, tmpPath string) error {
	rpath := filepath.Join(s.root, b.LibraryPath)

	if err := os.MkdirAll(rpath, 0755); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, filepath.Join(rpath, b.EpubFilename)); err != nil {
		return err
	}

	if err := writeMeta(filepath.Join(rpath, "meta.toml"), &b.Meta); err != nil {
		if rmErr := os.RemoveAll(rpath); rmErr != nil {
			log.Printf("store: Ingest cleanup failed for %s: %v", rpath, rmErr)
		}
		return err
	}

	return nil
}
