package store

import (
	"os"
	"path/filepath"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
)

func (l *Library) Ingest(id int64, book *epub.Book, tmpPath string) (*StoredBook, error) {
	cpath := canonicalPath(book.Authors, book.Title, id)
	rpath := filepath.Join(l.root, cpath)
	filename := epubFilename(book.Authors, book.Title)

	err := os.MkdirAll(rpath, 0755)
	if err != nil {
		return nil, err
	}

	if err = os.Rename(tmpPath, filepath.Join(rpath, filename)); err != nil {
		// TODO: any cleanup to do?
		return nil, err
	}

	now := time.Now()
	meta := &Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
		Status:       "unread",
		Rating:       0,
		CustomTags:   []string{},
	}

	if err := writeMeta(filepath.Join(rpath, "meta.toml"), meta); err != nil {
		_ = os.RemoveAll(rpath)
		return nil, err
	}
	return &StoredBook{
		ID:           id,
		LibraryPath:  cpath,
		EpubFilename: filename,
	}, nil
}
