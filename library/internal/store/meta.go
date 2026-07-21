package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// metaFilename is the per-book sidecar the store writes alongside each epub.
// Everything that needs the file goes through metaPath so the name is stated
// once — a rename stays a compile-time concern rather than a silent ENOENT.
const metaFilename = "meta.toml"

// metaPath returns the absolute path of the meta.toml sidecar for the book at
// loc.
func (s *Store) metaPath(loc model.Location) string {
	return s.AbsPath(loc.LibraryPath, metaFilename)
}

// ReadMeta reads the meta.toml sidecar for the book at loc.
func (s *Store) ReadMeta(loc model.Location) (*model.Meta, error) {
	return readMeta(s.metaPath(loc))
}

// WriteMeta atomically replaces the meta.toml sidecar for the book at loc.
func (s *Store) WriteMeta(loc model.Location, meta *model.Meta) error {
	return writeMeta(s.metaPath(loc), meta)
}

func readMeta(path string) (*model.Meta, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	meta := &model.Meta{}
	if err = toml.Unmarshal(buf, meta); err != nil {
		return nil, err
	}
	return meta, nil
}

func writeMeta(path string, meta *model.Meta) error {
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
