package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/ramblingenzyme/ebookfs/internal/atomicfs"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// metaFilename reaches the filesystem only through metaPath, so a rename is a
// compile-time concern rather than a silent ENOENT.
const metaFilename = "meta.toml"

func (s *Store) metaPath(loc book.File) string {
	return filepath.Join(s.AbsPath(loc.Dir()), metaFilename)
}

func (s *Store) ReadMeta(loc book.File) (*book.Meta, error) {
	return readMeta(s.metaPath(loc))
}

func (s *Store) writeMeta(loc book.File, meta *book.Meta) error {
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

	if err := validMeta(meta); err != nil {
		return nil, err
	}

	canonicalizeMeta(meta)
	// The file is hand-editable, and the index keys tags on (book, tag).
	meta.Tags = book.FirstOf(meta.Tags, func(t string) string { return t })

	return meta, nil
}

// validMeta holds a hand-edited sidecar to the rules an edit through the
// library passes. The rebuild indexes every book in one transaction, so a
// rating of nan fails the NOT NULL constraint and stops every startup.
//
// An empty status is allowed, since ingest writes one and book.NewBook
// defaults it.
func validMeta(m *book.Meta) error {
	if m.ID <= 0 {
		return fmt.Errorf("id must be positive, got %d", m.ID)
	}

	e := book.Edits{Rating: &m.Rating}
	if m.Status != "" {
		e.Status = &m.Status
	}

	if v := book.Validate(e, nil); v != nil {
		return v
	}

	return nil
}

// writeMeta renames over path, so a crash mid-write leaves the old sidecar
// intact rather than a truncated one.
func writeMeta(path string, meta *book.Meta) error {
	buf, err := toml.Marshal(meta)
	if err != nil {
		return err
	}

	return atomicfs.Write(path, tempPattern(filepath.Base(path)), buf)
}

// canonicalizeMeta pins the precision and zone of the timestamps the index
// can round-trip. The returned value is what a later Get reports.
func canonicalizeMeta(m *book.Meta) {
	m.DateAdded = m.DateAdded.UTC().Truncate(time.Second)
	m.DateModified = m.DateModified.UTC().Truncate(time.Second)
}
