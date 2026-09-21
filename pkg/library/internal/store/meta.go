package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// metaFilename reaches the filesystem only through metaPath, so a rename is a
// compile-time concern rather than a silent ENOENT.
const metaFilename = "meta.toml"

func (s *Store) metaPath(loc book.Location) string {
	return filepath.Join(s.AbsPath(loc.Dir()), metaFilename)
}

func (s *Store) ReadMeta(loc book.Location) (*book.Meta, error) {
	return readMeta(s.metaPath(loc))
}

func (s *Store) writeMeta(loc book.Location, meta *book.Meta) error {
	return writeMeta(s.metaPath(loc), meta)
}

// readMeta and writeMeta take a path rather than a Location, which keeps the
// file handling separable from the layout metaPath owns. Tests reach them
// directly for a missing file, a malformed sidecar and an unwritable directory,
// none of which is reachable through a Store worth building.
func readMeta(path string) (*book.Meta, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	meta := &book.Meta{}
	if err = toml.Unmarshal(buf, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

// writeMeta renames over path, so a crash mid-write leaves the old sidecar
// intact rather than a truncated one.
func writeMeta(path string, meta *book.Meta) error {
	buf, err := toml.Marshal(meta)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return err
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	if _, err = tmp.Write(buf); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
