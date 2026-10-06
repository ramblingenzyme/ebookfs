package epub

import (
	"io"
	"os"

	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

var (
	ErrClosed          = epubfile.ErrClosed
	ErrContainer       = epubfile.ErrContainer
	ErrNoRootfile      = epubfile.ErrNoRootfile
	ErrRootfileMissing = epubfile.ErrRootfileMissing
	ErrNotEpub         = epubfile.ErrNotEpub
	ErrNoCover         = epubfile.ErrNoCover
)

// EpubReader is an open epub. It shows the book as it was when opened.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	OPF() ([]byte, error)
	Cover() ([]byte, error)
}

// coverPath is taken from the index so that opening a reader never parses the
// package document. The 9P read path opens a reader for every fid.
type reader struct {
	*epubfile.File
	coverPath string
}

// OpenReader opens the epub at epubPath. coverPath is the book's
// Bib.CoverPath, or "" if it has no cover.
func OpenReader(epubPath, coverPath string) (EpubReader, error) {
	f, err := epubfile.OpenFile(epubPath)
	if err != nil {
		return nil, err
	}
	return &reader{File: f, coverPath: coverPath}, nil
}

// OpenReaderFromFile opens the epub from an already-open file. The caller
// retains ownership of f; on error, f is left open. The path is used only for
// error messages and may be empty.
func OpenReaderFromFile(f *os.File, path, coverPath string) (EpubReader, error) {
	ef, err := epubfile.OpenFromFile(f, path)
	if err != nil {
		return nil, err
	}
	return &reader{File: ef, coverPath: coverPath}, nil
}

func (r *reader) OPF() ([]byte, error) { return r.ReadEntry(r.PackagePath()) }

func (r *reader) Cover() ([]byte, error) {
	// Checked first, so a closed reader returns ErrClosed, not ErrNoCover.
	if r.Closed() {
		return nil, epubfile.ErrClosed
	}
	if r.coverPath == "" {
		return nil, epubfile.ErrNoCover
	}
	return r.ReadEntry(r.coverPath)
}
