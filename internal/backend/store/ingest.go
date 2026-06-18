package store

import (
	"log"
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// Ingest materializes a book on disk at loc: it creates the book directory,
// moves the staged epub at epubPath into place as loc.EpubFilename, and writes
// the meta.toml sidecar from meta. The caller computes loc (see Layout).
func (s *Store) Ingest(epubPath string, loc model.Location, meta *model.Meta) error {
	rpath := filepath.Join(s.root, loc.LibraryPath)

	if err := os.MkdirAll(rpath, 0755); err != nil {
		return err
	}

	if err := os.Rename(epubPath, filepath.Join(rpath, loc.EpubFilename)); err != nil {
		return err
	}

	if err := writeMeta(filepath.Join(rpath, "meta.toml"), meta); err != nil {
		if rmErr := os.RemoveAll(rpath); rmErr != nil {
			log.Printf("store: Ingest cleanup failed for %s: %v", rpath, rmErr)
		}
		return err
	}

	return nil
}
