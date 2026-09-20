package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// metaFilename is the per-book sidecar the store writes alongside each epub.
// Everything that needs the file goes through metaPath so the name is stated
// once, so a rename stays a compile-time concern rather than a silent ENOENT.
const metaFilename = "meta.toml"

func (s *Store) metaPath(loc book.Location) string {
	return filepath.Join(s.root, loc.Dir(), metaFilename)
}

func (s *Store) ReadMeta(loc book.Location) (*book.Meta, error) {
	return readMeta(s.metaPath(loc))
}

func (s *Store) writeMeta(loc book.Location, meta *book.Meta) error {
	return writeMeta(s.metaPath(loc), meta)
}

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

// writeMeta replaces path atomically: the sidecar is written to a temp file in
// the same directory, synced, then renamed over. A crash mid-write leaves the
// old sidecar intact rather than a truncated one.
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
