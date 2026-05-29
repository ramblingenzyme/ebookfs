package store

import (
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Meta mirrors the meta.toml sidecar schema.
type Meta struct {
	ID           int64     `toml:"id"`
	DateAdded    time.Time `toml:"date_added"`
	DateModified time.Time `toml:"date_modified"`
	// TODO: make this an enum with a string representation?
	Status     string   `toml:"status"` // unread | reading | read | abandoned
	Rating     int      `toml:"rating"` // 0-5, 0 = unrated
	CustomTags []string `toml:"custom_tags"`
}

// ReadMeta reads the meta.toml sidecar for b.
func (l *Library) ReadMeta(b *StoredBook) (*Meta, error) {
	return readMeta(l.absPath(b, "meta.toml"))
}

// WriteMeta atomically replaces the meta.toml sidecar for b.
func (l *Library) WriteMeta(b *StoredBook, meta *Meta) error {
	return writeMeta(l.absPath(b, "meta.toml"), meta)
}

// readMeta opens path and decodes its contents into a Meta struct.
func readMeta(path string) (*Meta, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	meta := &Meta{}
	err = toml.Unmarshal(buf, meta)
	if err != nil {
		return nil, err
	}

	return meta, nil
}

// writeMeta atomically writes meta as TOML to path.
func writeMeta(path string, meta *Meta) error {
	// 1. Encode meta to a bytes.Buffer using BurntSushi/toml encoder.
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

	_, err = tmp.Write(buf)
	if err != nil {
		return err
	}

	err = tmp.Sync()
	if err != nil {
		return err
	}

	return os.Rename(tmp.Name(), path)
}
