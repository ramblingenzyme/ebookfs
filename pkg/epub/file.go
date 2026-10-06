package epub

import (
	"archive/zip"
	"errors"
	"os"
)

// ErrClosed is returned by a File's methods after Close.
var ErrClosed = errors.New("epub file is closed")

// File is an open epub archive whose mimetype has been checked and whose
// package document has been found. The package document is not parsed; Open does that.
type File struct {
	path   string
	f      *os.File
	a      *archive
	closed bool
}

// OpenFile opens the epub at path without parsing its package document.
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

// OpenFromFile opens the epub from an already-open file. The caller retains
// ownership of f; on error, f is left open. The path is used only for error
// messages and may be empty.
func OpenFromFile(f *os.File, path string) (_ *File, err error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, notEpub(path, err)
	}
	a, err := openArchive(zr)
	if err != nil {
		return nil, err
	}
	if err := a.validate(); err != nil {
		return nil, err
	}
	return &File{path: path, f: f, a: a}, nil
}

// Closed reports whether Close has been called. A handle often outlives the
// book it was opened for, so a wrapper needs to tell a closed file apart from
// its own reasons for returning nothing.
func (f *File) Closed() bool { return f.closed }

// PackagePath returns the package document's path inside the zip. If the
// container lists several, it is the first one the archive holds.
func (f *File) PackagePath() string { return f.a.opf }

// ReadEntry returns an entry's contents. If two entries share a name, it reads the first.
//
// File has no OPF method on purpose. Book embeds File, and a File.OPF would
// return the package document without the Book's unsaved edits.
func (f *File) ReadEntry(name string) ([]byte, error) {
	if f.closed {
		return nil, ErrClosed
	}
	return f.a.read(name)
}

func (f *File) has(name string) bool {
	if f.closed {
		return false
	}
	return f.a.has(name)
}

// Size returns an entry's uncompressed size without decompressing it, or 0 if
// there is no such entry.
func (f *File) Size(name string) int64 {
	if f.closed {
		return 0
	}
	return f.a.size(name)
}

// ReadAt implements io.ReaderAt on the raw bytes of the epub file.
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
