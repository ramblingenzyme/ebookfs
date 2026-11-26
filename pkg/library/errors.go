package library

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/fsutil"
)

// ErrBookNotFound is wrapped into the error of any call naming a book id the
// index does not hold. Any other error from that call is an index or store failure.
var ErrBookNotFound = errors.New("no such book")

// ErrLibraryLocked is returned by Open when another process holds the flock on
// the library root. The wrapped error names the holder as "<hostname>:<pid>".
var ErrLibraryLocked = errors.New("library is locked by another process")

// ErrSMBFilesystem is wrapped into Open's error when the library root or the
// index is on SMB/CIFS and WithAllowSMB was not passed.
var ErrSMBFilesystem = fsutil.ErrSMBFilesystem

// ErrDuplicate is wrapped into an ingest's error when the library already
// holds a book with the same title and set of authors.
var ErrDuplicate = errors.New("book already in library")

// ErrDuplicateOnDisk is wrapped into an ingest's error when the book is not
// indexed but its file is already in the tree, a book the indexer skipped.
// Ingesting anyway would leave two copies, one invisible. Reindex or remove the existing file.
var ErrDuplicateOnDisk = errors.New("book already on disk but not indexed")

// ErrUnknownFormat means no registered format detected the file.
var ErrUnknownFormat = formats.ErrUnknownFormat

// ErrInvalidBook means the format was detected but the file is corrupt or
// invalid for it. The wrapped error names the specific failure.
var ErrInvalidBook = formats.ErrInvalidBook

// ErrClosed is returned by every Reader method after Close. A 9P client can
// hold a fid across a re-ingest, so this is an ordinary outcome, not a caller's mistake.
var ErrClosed = formats.ErrClosed

// ErrNoCover is returned by Reader.Cover when the book has no cover.
var ErrNoCover = formats.ErrNoCover
