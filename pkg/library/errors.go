package library

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// ErrBookNotFound is wrapped into the error returned by any operation that
// addresses a book id the index does not hold. Callers distinguish it with
// errors.Is; anything else from the same call is an index or store failure.
var ErrBookNotFound = errors.New("no such book")

// ErrDuplicate is wrapped into the error an ingest returns when the library
// already holds a book with the same title and the same set of authors.
var ErrDuplicate = errors.New("book already in library")

// ErrDuplicateOnDisk is wrapped into the error an ingest returns when no
// indexed book matches, but a file for these authors and this title is already
// in the library tree, a book the indexer skipped. Ingesting anyway would
// leave two copies on disk, one of them invisible. Reindex or remove the
// existing file.
var ErrDuplicateOnDisk = errors.New("book already on disk but not indexed")

// The epub package's own errors, surfaced here because they reach this
// package's callers: a bad upload fails Ingest, and a fid held across a
// re-ingest fails a read on an EpubReader. They are the same values, so
// errors.Is matches whichever package a caller names them from.
//
// ErrNotEpub means the file is not an epub at all, either not a zip or not
// carrying the mimetype OCF 3.3 §4.3.3 fixes. The other three each name where
// the trail from the OCF container to the package document stops.
var (
	ErrNotEpub         = epub.ErrNotEpub
	ErrContainer       = epub.ErrContainer
	ErrNoRootfile      = epub.ErrNoRootfile
	ErrRootfileMissing = epub.ErrRootfileMissing
)

// ErrClosed is returned by every EpubReader accessor after Close. Reads arrive
// from the 9P layer on a handle a client may hold across a re-ingest, so a
// use-after-close is an ordinary end for one rather than a caller's mistake.
var ErrClosed = epub.ErrClosed

// ErrNoCover is returned by EpubReader.Cover when the book's package document
// points at no cover image.
var ErrNoCover = epub.ErrNoCover
