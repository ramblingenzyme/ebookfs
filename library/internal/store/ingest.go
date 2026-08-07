package store

import (
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Ingest materializes a book on disk at loc: it creates the book directory,
// moves the staged epub at epubPath into place as loc.Filename(), and writes
// the meta.toml sidecar from meta. The caller computes loc (see Layout).
func (s *Store) Ingest(epubPath string, loc model.Location, meta *model.Meta) (drift.PathInfo, error) {
	rpath := filepath.Join(s.root, loc.Dir())

	if err := os.MkdirAll(rpath, 0755); err != nil {
		return drift.PathInfo{}, err
	}

	if err := os.Rename(epubPath, filepath.Join(rpath, loc.Filename())); err != nil {
		return drift.PathInfo{}, err
	}

	if err := writeMeta(filepath.Join(rpath, metaFilename), meta); err != nil {
		return drift.PathInfo{}, err
	}

	return s.Stat(loc)
}
