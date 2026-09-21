package store

import (
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
)

func (s *Store) Ingest(epubPath string, loc book.Location, meta *book.Meta) (drift.PathInfo, error) {
	dir := s.AbsPath(loc.Dir())

	if err := os.MkdirAll(dir, 0755); err != nil {
		return drift.PathInfo{}, err
	}

	if err := os.Rename(epubPath, filepath.Join(dir, loc.Filename())); err != nil {
		return drift.PathInfo{}, err
	}

	if err := s.writeMeta(loc, meta); err != nil {
		return drift.PathInfo{}, err
	}

	return s.Stat(loc)
}
