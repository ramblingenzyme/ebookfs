package pdf

import (
	"errors"
	"fmt"
	"os"

	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/cos"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf/internal/xref"
)

var (
	// ErrNotPDF wraps a failure to read the file as PDF syntax.
	ErrNotPDF = errors.New("not a valid pdf")
	// ErrClosed is returned by a File's methods after Close.
	ErrClosed = errors.New("pdf file is closed")
)

// File is an open PDF whose header, cross-reference sections and trailer have
// been read. No object has been resolved; Open does that.
type File struct {
	path   string
	f      *os.File
	size   int64
	x      *xref.Table
	closed bool
}

// OpenFile opens the PDF at path without reading its metadata.
func OpenFile(path string) (_ *File, err error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			f.Close()
		}
	}()

	return OpenFromFile(f, path)
}

// OpenFromFile opens the PDF from an already-open file. The caller retains
// ownership of f; on error, f is left open. The path is used only for error
// messages and may be empty.
func OpenFromFile(f *os.File, path string) (*File, error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}

	x, err := xref.Open(f, fi.Size())
	if err != nil {
		return nil, notPDF(path, err)
	}

	return &File{path: path, f: f, size: fi.Size(), x: x}, nil
}

// notPDF wraps a syntax error in ErrNotPDF. Other errors, such as a failed
// read, say nothing about the contents and pass through unchanged.
func notPDF(path string, err error) error {
	if errors.Is(err, cos.ErrMalformed) {
		return fmt.Errorf("%w: %s: %w", ErrNotPDF, path, err)
	}

	return err
}

// Closed reports whether Close has been called. A handle often outlives the
// document it was opened for, so a wrapper needs to tell a closed file apart
// from its own reasons for returning nothing.
func (f *File) Closed() bool { return f.closed }

// ReadAt implements io.ReaderAt on the raw bytes of the PDF file.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, ErrClosed
	}

	return f.f.ReadAt(p, off)
}

// Close closes the file. Calling it again returns ErrClosed.
func (f *File) Close() error {
	if f.closed {
		return ErrClosed
	}

	f.closed = true

	return f.f.Close()
}
