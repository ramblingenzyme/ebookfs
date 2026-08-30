package epub

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
)

var ErrClosed = errors.New("epub reader is closed")

// EpubReader provides access to a book's epub content from an open handle.
// The handle keeps the file and zip central directory open so repeated calls
// to OPF or Cover avoid re-reading. Close when done.
//
// Implementations of methods that return an EpubReader (Library.Content,
// Exporter.Open) must return a non-nil reader if err is nil; a nil reader with
// a nil error is a contract violation.
//
// An EpubReader is a snapshot of the book at open time — it does not track
// edits. After a concurrent Edit, call Library.Content again for a handle
// that reads from the updated file.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	OPF() ([]byte, error)   // OPF XML from the open epub
	Cover() ([]byte, error) // cover image from the open epub
}

// Reader provides random access to an epub's contents through a single open
// file handle. The underlying *os.File and zip.Reader stay open, so repeated
// calls to OPF or Cover avoid re-reading the zip central directory.
//
// Reader satisfies EpubReader (io.ReaderAt + io.Closer + OPF + Cover).
type Reader struct {
	f *os.File
	// a indexes the entries and holds the resolved package document path. The
	// same seam Parse and Rewrite use, so all three resolve an entry alike.
	a         *archive
	coverPath string // zip-relative path to cover image; empty if none
	closed    bool   // true after Close; accessors return ErrClosed
}

// OpenReader opens the epub at epubPath and reads the zip central directory.
// coverPath is the zip-relative path to the cover image (from book.Bib.CoverPath);
// it may be empty. The returned reader keeps the file open; the caller must call
// Close. The reader is non-nil iff err is nil.
func OpenReader(epubPath, coverPath string) (EpubReader, error) {
	f, err := os.Open(epubPath)
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		f.Close()
		return nil, notEpub(epubPath, err)
	}
	a, err := openArchive(zr)
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := a.validate(); err != nil {
		f.Close()
		return nil, err
	}
	return &Reader{f: f, a: a, coverPath: coverPath}, nil
}

// ReadAt implements io.ReaderAt on the raw epub bytes.
func (r *Reader) ReadAt(p []byte, off int64) (int, error) {
	if r.closed {
		return 0, ErrClosed
	}
	return r.f.ReadAt(p, off)
}

// Close releases the underlying file. The zip.Reader becomes invalid.
// It is safe to call multiple times — subsequent calls return ErrClosed.
func (r *Reader) Close() error {
	if r.closed {
		return ErrClosed
	}
	r.closed = true
	return r.f.Close()
}

// OPF returns the raw OPF XML bytes, decompressing the entry on demand.
func (r *Reader) OPF() ([]byte, error) {
	if r.closed {
		return nil, ErrClosed
	}
	return r.a.read(r.a.opf)
}

// Cover returns the cover image bytes from the already-open zip. When coverPath
// is empty (no cover in the epub) Cover returns an error.
func (r *Reader) Cover() ([]byte, error) {
	if r.closed {
		return nil, ErrClosed
	}
	if r.coverPath == "" {
		return nil, fmt.Errorf("no cover in epub")
	}
	return r.a.read(r.coverPath)
}
