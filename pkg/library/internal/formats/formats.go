// Package formats defines the Format interface and a registry that dispatches
// by book.Format. Each book-file format (epub, pdf, cbz) provides an
// implementation.
package formats

import (
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Reader is an open book file. It shows the book as it was when opened.
type Reader interface {
	io.ReaderAt
	io.Closer
	Cover() ([]byte, error)
}

// ErrClosed is returned by every Reader method after Close.
var ErrClosed = errors.New("file is closed")

// ErrNoCover is returned by Reader.Cover when the book has no cover.
var ErrNoCover = errors.New("no cover")

// ErrUnknownFormat means no registered format detected the file.
var ErrUnknownFormat = errors.New("unrecognized file format")

// ErrInvalidBook means the format was detected but the file is corrupt or
// invalid for it.
var ErrInvalidBook = errors.New("invalid book file")

// ErrUnsupported means the format does not support this operation.
var ErrUnsupported = errors.New("format does not support this operation")

// IsIO reports whether err came from reading the file rather than from its
// contents. pkg/epub passes I/O failures through unwrapped, so they keep their
// *fs.PathError.
func IsIO(err error) bool {
	var pe *fs.PathError
	return errors.As(err, &pe)
}

// Format encapsulates the operations that differ per book-file format.
type Format interface {
	// Format returns the enum value that identifies this format.
	Format() book.Format

	// Detect reports whether path is a file of this format. Implementations
	// read only enough of the file to distinguish it from other formats. A
	// file of this format that is broken still reports true, so Parse can
	// name the fault. The error is for I/O only.
	//
	// ponytail: consider taking io.ReaderAt instead of a path, so the
	// registry does not open the file repeatedly. Parse must match.
	Detect(path string) (bool, error)

	// Parse reads the bibliographic metadata from the file at path.
	Parse(path string) (*book.Bib, error)

	// Rewrite applies edits to the file at path and returns the updated Bib.
	// Returns ErrUnsupported if the format does not support in-place edits.
	Rewrite(path string, current *book.Book, e book.Edits) (*book.Bib, error)

	// OpenReader opens the file at path for content access. coverPath is
	// Bib.CoverPath, or "" if the book has no cover.
	OpenReader(path, coverPath string) (Reader, error)
}

var registry []Format

// Register adds a format implementation. Called from init() in format packages.
//
// ponytail: init() order follows the import graph, so Detect's priority is
// implicit. Replace with an explicit ordered list once a second zip-based
// format (cbz) would also accept an epub.
func Register(f Format) {
	registry = append(registry, f)
}

// ByFormat returns the Format implementation for the given enum value, or an
// error wrapping ErrUnknownFormat if none is registered.
func ByFormat(bf book.Format) (Format, error) {
	for _, f := range registry {
		if f.Format() == bf {
			return f, nil
		}
	}
	return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, bf)
}

// Detect returns the first registered format whose Detect accepts path. Order
// is registration order, so a format whose check is stricter registers before
// a looser one that also accepts its files.
func Detect(path string) (Format, error) {
	for _, f := range registry {
		ok, err := f.Detect(path)
		if err != nil {
			return nil, err
		}
		if ok {
			return f, nil
		}
	}
	return nil, ErrUnknownFormat
}
