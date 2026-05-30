package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

// ReadMeta reads the meta.toml sidecar for the book at libraryPath.
func (s *Store) ReadMeta(libraryPath string) (*model.Meta, error) {
	return readMeta(filepath.Join(s.root, libraryPath, "meta.toml"))
}

// WriteMeta atomically replaces the meta.toml sidecar for the book at libraryPath.
func (s *Store) WriteMeta(libraryPath string, meta *model.Meta) error {
	return writeMeta(filepath.Join(s.root, libraryPath, "meta.toml"), meta)
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
