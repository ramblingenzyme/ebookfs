// Package formats defines the Format interface and a registry that dispatches
// by book.Format. Each book-file format (epub, pdf, cbz) provides an
// implementation.
package formats

import (
	"errors"
	"io"

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

// ErrUnsupported means the format does not support this operation (e.g.,
// rewriting metadata for a format that lacks in-place edit support).
var ErrUnsupported = errors.New("format does not support this operation")

// ErrUnknownFormat means no registered format detected the file.
var ErrUnknownFormat = errors.New("unrecognized file format")

// ErrInvalidBook means the format was detected but the file is corrupt or
// invalid for it.
var ErrInvalidBook = errors.New("invalid book file")

// Format encapsulates the operations that differ per book-file format.
type Format interface {
	// Format returns the enum value that identifies this format.
	Format() book.Format

	// Detect reports whether path is a file of this format. Implementations
	// read only enough of the file to distinguish it from other formats.
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

	// Extension returns the file extension including the leading dot (".epub").
	Extension() string

	// MediaType returns the MIME type for this format.
	MediaType() string
}

var registry []Format

// Register adds a format implementation. Called from init() in format packages.
func Register(f Format) {
	registry = append(registry, f)
}

// All returns the registered formats in priority order (extension preference).
func All() []Format { return registry }

// ByFormat returns the Format implementation for the given enum value.
func ByFormat(bf book.Format) (Format, bool) {
	for _, f := range registry {
		if f.Format() == bf {
			return f, true
		}
	}
	return nil, false
}

// Detect iterates registered formats and returns the first whose Detect
// reports true. Extension is used as a hint to order the check.
func Detect(path string) (Format, error) {
	ext := extensionOf(path)
	ordered := orderedBy(ext)
	for _, f := range ordered {
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

func extensionOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '.' {
			return toLower(path[i:])
		}
		if path[i] == '/' || path[i] == '\\' {
			break
		}
	}
	return ""
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

// orderedBy returns formats with the given extension first, then the rest.
func orderedBy(ext string) []Format {
	if ext == "" {
		return registry
	}
	ordered := make([]Format, 0, len(registry))
	var rest []Format
	for _, f := range registry {
		if f.Extension() == ext {
			ordered = append(ordered, f)
		} else {
			rest = append(rest, f)
		}
	}
	return append(ordered, rest...)
}
