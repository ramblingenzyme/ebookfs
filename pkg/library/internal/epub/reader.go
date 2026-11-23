package epub

import (
	"os"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

var (
	ErrContainer       = epubfile.ErrContainer
	ErrNoRootfile      = epubfile.ErrNoRootfile
	ErrRootfileMissing = epubfile.ErrRootfileMissing
	ErrNotEpub         = epubfile.ErrNotEpub
)

// coverPath is taken from the index so that opening a reader never parses the
// package document. The 9P read path opens a reader for every fid.
type reader struct {
	*epubfile.File
	coverPath string
}

// OpenReader opens the epub at path. coverPath is the book's
// Bib.CoverPath, or "" if it has no cover.
func OpenReader(path, coverPath string) (*reader, error) {
	f, err := epubfile.OpenFile(path)
	if err != nil {
		return nil, err
	}
	return &reader{File: f, coverPath: coverPath}, nil
}

// OpenReaderFromFile opens the epub from an already-open file. The caller
// retains ownership of f; on error, f is left open. The path is used only for
// error messages and may be empty.
func OpenReaderFromFile(f *os.File, path, coverPath string) (*reader, error) {
	ef, err := epubfile.OpenFromFile(f, path)
	if err != nil {
		return nil, err
	}
	return &reader{File: ef, coverPath: coverPath}, nil
}

func (r *reader) ReadAt(p []byte, off int64) (int, error) {
	if r.Closed() {
		return 0, formats.ErrClosed
	}
	return r.File.ReadAt(p, off)
}

func (r *reader) Close() error {
	if r.Closed() {
		return formats.ErrClosed
	}
	return r.File.Close()
}

func (r *reader) OPF() ([]byte, error) {
	if r.Closed() {
		return nil, formats.ErrClosed
	}
	return r.ReadEntry(r.PackagePath())
}

func (r *reader) Cover() ([]byte, error) {
	if r.Closed() {
		return nil, formats.ErrClosed
	}
	if r.coverPath == "" {
		return nil, formats.ErrNoCover
	}
	return r.ReadEntry(r.coverPath)
}
