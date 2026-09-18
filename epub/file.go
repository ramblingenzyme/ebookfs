package epub

import (
	"archive/zip"
	"errors"
	"os"
)

var ErrClosed = errors.New("epub file is closed")

// File is an open epub archive: the zip central directory, a validated
// mimetype, and the package document's path resolved from the OCF container.
// The package document itself is not parsed, so opening one costs no XML work
// — Open does that.
//
// The underlying file handle stays open, so repeated reads avoid re-reading the
// central directory. Close when done.
type File struct {
	path string
	f    *os.File
	// a indexes the entries and holds the resolved package document path. The
	// same seam Open and Save use, so all three resolve an entry alike.
	a      *archive
	closed bool // true after Close; accessors return ErrClosed
}

// OpenFile opens the epub at path and reads the zip central directory. The
// returned File keeps the handle open; the caller must Close it. It is non-nil
// iff err is nil.
func OpenFile(path string) (*File, error) {
	f, err := os.Open(path)
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
		return nil, notEpub(path, err)
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
	return &File{path: path, f: f, a: a}, nil
}

// Closed reports whether Close has been called. A use-after-close is an
// ordinary way for a long-lived handle to end — a 9P client can hold a fid
// across a re-ingest — so a wrapper needs to tell that apart from its own
// reasons for having nothing to return.
func (f *File) Closed() bool { return f.closed }

// PackagePath returns the package document's zip-relative path, as the OCF
// container declares it and the archive confirms it. Where a container names
// several rootfiles, this is the one every read and write resolves.
func (f *File) PackagePath() string { return f.a.opf }

// ReadEntry returns an archive entry's bytes, decompressing on demand. Where a
// name appears twice, this is the copy every other method means.
//
// There is deliberately no OPF method: Book embeds a *File and serializes its
// pending edits, so two methods of that name would have the same signature and
// opposite freshness.
func (f *File) ReadEntry(name string) ([]byte, error) {
	if f.closed {
		return nil, ErrClosed
	}
	return f.a.read(name)
}

// Has reports whether the archive carries an entry by that name.
func (f *File) Has(name string) bool {
	if f.closed {
		return false
	}
	return f.a.has(name)
}

// Size returns an entry's uncompressed size from the zip central directory, so
// nothing is decompressed. An absent entry is 0.
func (f *File) Size(name string) int64 {
	if f.closed {
		return 0
	}
	return f.a.size(name)
}

// ReadAt implements io.ReaderAt on the raw epub bytes.
func (f *File) ReadAt(p []byte, off int64) (int, error) {
	if f.closed {
		return 0, ErrClosed
	}
	return f.f.ReadAt(p, off)
}

// Close releases the underlying file. The zip.Reader becomes invalid.
// It is safe to call multiple times — subsequent calls return ErrClosed.
func (f *File) Close() error {
	if f.closed {
		return ErrClosed
	}
	f.closed = true
	return f.f.Close()
}
