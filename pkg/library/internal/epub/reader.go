package epub

import (
	"io"

	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

// Re-exported so the library's callers need not import the epub package to name
// an error this one returns. library aliases these in turn: a bad upload fails
// Ingest with ErrNotEpub, and a fid held across a re-ingest fails a read with
// ErrClosed, so both reach a caller that never names epub at all.
var (
	ErrClosed          = epubfile.ErrClosed
	ErrContainer       = epubfile.ErrContainer
	ErrNoRootfile      = epubfile.ErrNoRootfile
	ErrRootfileMissing = epubfile.ErrRootfileMissing
	ErrNotEpub         = epubfile.ErrNotEpub
)

// EpubReader provides access to a book's epub content from an open handle.
// The handle keeps the file and zip central directory open so repeated calls
// to OPF or Cover avoid re-reading. Close when done.
//
// Implementations of methods that return an EpubReader (Library.Content,
// Exporter.Open) must return a non-nil reader if err is nil; a nil reader with
// a nil error is a contract violation.
//
// An EpubReader is a snapshot of the book at open time. It does not track
// edits. After a concurrent Edit, call Library.Content again for a handle
// that reads from the updated file.
type EpubReader interface {
	io.ReaderAt
	io.Closer
	OPF() ([]byte, error)   // OPF XML from the open epub
	Cover() ([]byte, error) // cover image from the open epub
}

// reader is an open epub, plus the cover path the index holds for it. It is
// unexported because OpenReader hands back the EpubReader interface and nothing
// outside constructs one.
//
// The path comes from outside because the epub package resolves it only when it
// parses the package document, and this handle deliberately does not: it serves
// the 9P read path, where every request would otherwise pay for an XML parse.
// The index already recorded the path at ingest, so the parse is redundant.
type reader struct {
	*epubfile.File
	coverPath string // zip-relative path to cover image; empty if none
}

// OpenReader opens the epub at epubPath and reads the zip central directory.
// coverPath is the zip-relative path to the cover image (from book.Bib.CoverPath);
// it may be empty. The returned reader keeps the file open; the caller must call
// Close. The reader is non-nil iff err is nil.
func OpenReader(epubPath, coverPath string) (EpubReader, error) {
	f, err := epubfile.OpenFile(epubPath)
	if err != nil {
		return nil, err
	}
	return &reader{File: f, coverPath: coverPath}, nil
}

// OPF returns the raw OPF XML bytes, decompressing the entry on demand.
func (r *reader) OPF() ([]byte, error) { return r.ReadEntry(r.PackagePath()) }

// Cover returns the cover image bytes from the already-open zip, ErrNoCover
// when coverPath is empty because the epub carries none, and ErrClosed after
// Close.
func (r *reader) Cover() ([]byte, error) {
	// Before the coverPath test, so a closed handle reports itself rather than
	// the cover it can no longer reach; every other accessor answers alike.
	if r.Closed() {
		return nil, epubfile.ErrClosed
	}
	if r.coverPath == "" {
		return nil, epubfile.ErrNoCover
	}
	return r.ReadEntry(r.coverPath)
}
