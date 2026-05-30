package store

import (
	"os"
	"path/filepath"

	"github.com/ramblingenzyme/ebookfs/internal/epub"
	"github.com/ramblingenzyme/ebookfs/internal/model"
)

func (s *Store) Ingest(id int64, book *epub.Book, tmpPath string, meta *model.Meta) (libraryPath, epubFilename string, err error) {
	libraryPath = CanonicalPath(book.Authors, book.Title, id)
	epubFilename = EpubFilename(book.Authors, book.Title)
	rpath := filepath.Join(s.root, libraryPath)

	if err = os.MkdirAll(rpath, 0755); err != nil {
		return "", "", err
	}

	if err = os.Rename(tmpPath, filepath.Join(rpath, epubFilename)); err != nil {
		return "", "", err
	}

	if err = writeMeta(filepath.Join(rpath, "meta.toml"), meta); err != nil {
		_ = os.RemoveAll(rpath)
		return "", "", err
	}

	return libraryPath, epubFilename, nil
}
