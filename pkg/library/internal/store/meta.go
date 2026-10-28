package store

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/ramblingenzyme/ebookfs/internal/atomicfs"
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

func readMeta(path string) (*book.Meta, error) {
	buf, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	meta := &book.Meta{}
	if err = toml.Unmarshal(buf, meta); err != nil {
		return nil, err
	}
	// The file is hand-editable, and the index keys tags on (book, tag).
	meta.Tags = book.FirstOf(meta.Tags, func(t string) string { return t })
	return meta, nil
}

// writeMeta renames over path, so a crash mid-write leaves the old sidecar
// intact rather than a truncated one.
func writeMeta(path string, meta *book.Meta) error {
	buf, err := toml.Marshal(meta)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	pattern := filepath.Base(path)
	return atomicfs.Write(dir, pattern, func(f *os.File) error {
		_, err := f.Write(buf)
		return err
	}, path)
}
