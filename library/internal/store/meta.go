package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ReadMeta reads the meta.toml sidecar for the book at loc.
func (s *Store) ReadMeta(loc model.Location) (*model.Meta, error) {
	return readMeta(s.AbsPath(loc.LibraryPath, "meta.toml"))
}

// WriteMeta atomically replaces the meta.toml sidecar for the book at loc.
func (s *Store) WriteMeta(loc model.Location, meta *model.Meta) error {
	return writeMeta(s.AbsPath(loc.LibraryPath, "meta.toml"), meta)
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
