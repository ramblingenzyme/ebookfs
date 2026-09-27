package library

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// ErrBookNotFound is wrapped into the error of any call naming a book id the
// index does not hold. Any other error from that call is an index or store failure.
var ErrBookNotFound = errors.New("no such book")

// ErrDuplicate is wrapped into an ingest's error when the library already
// holds a book with the same title and set of authors.
var ErrDuplicate = errors.New("book already in library")

// ErrDuplicateOnDisk is wrapped into an ingest's error when the book is not
// indexed but its file is already in the tree, a book the indexer skipped.
// Ingesting anyway would leave two copies, one invisible. Reindex or remove the existing file.
var ErrDuplicateOnDisk = errors.New("book already on disk but not indexed")

// The epub package's errors, re-exported because they reach this package's
// callers. They are the same values, so errors.Is matches either name.
//
// ErrNotEpub means the file is not a zip, or lacks the mimetype that
// OCF 3.3 §4.3.3 fixes. The other three name where the path from the container
// to the package document breaks.
var (
	ErrNotEpub         = epub.ErrNotEpub
	ErrContainer       = epub.ErrContainer
	ErrNoRootfile      = epub.ErrNoRootfile
	ErrRootfileMissing = epub.ErrRootfileMissing
)

// ErrClosed is returned by every EpubReader method after Close. A 9P client can
// hold a fid across a re-ingest, so this is an ordinary outcome, not a caller's mistake.
var ErrClosed = epub.ErrClosed

// ErrNoCover is returned by EpubReader.Cover when the book has no cover.
var ErrNoCover = epub.ErrNoCover
