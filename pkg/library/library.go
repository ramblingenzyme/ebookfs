package library

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

type Edits = book.Edits
type ValidationError = book.ValidationError
type FieldError = book.FieldError
type Query = index.Query
type Order = index.Order
type Stats = index.Stats
type Author = book.Author
type Series = book.SeriesRef
type EpubReader = epub.EpubReader

// StatusList renders the reading-status vocabulary for a message an operator
// reads, so a validation error and the ctl help name the same set.
var StatusList = book.StatusList

const (
	OrderSortTitle    = index.OrderSortTitle
	OrderDateAdded    = index.OrderDateAdded
	OrderDateModified = index.OrderDateModified
	OrderRating       = index.OrderRating
	OrderPubdate      = index.OrderPubdate
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
// ErrNotEpub means the file is not an epub at all: not a zip, or not carrying
// the mimetype OCF 3.3 §4.3.3 fixes. The other three are an epub whose OCF
// container does not lead to a package document, which say where the trail
// stops.
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

// Exporter produces the rsync-export rendition of a book for the reader/ view.
// It is the single swap point between serving the original epub and a converted
// kepub: the Library returns the appropriate implementation based on config.
//
// Includes decides which books belong in the reader and Dirname decides how
// they group. Both are policy, so they sit with the rendition methods here and
// fs/views/reader.go only renders the result. Includes is a predicate rather
// than an exposed status list, so the policy can change to tag-based or size
// caps without touching the frontend.
type Exporter interface {
	// Open returns a handle to the book's export rendition, holding a snapshot
	// as Book does. The returned reader is non-nil iff err is nil.
	Open(*Book) (EpubReader, error)
	Size(*Book) (int64, bool) // cheap; 9P stat length, false when cold
	Warm(*Book)               // non-blocking proactive warm hint
	Filename(*Book) string    // FAT-safe export name
	Dirname(*Book) string     // FAT-safe export directory name
	Includes(*Book) bool      // whether the book appears in the reader view
}

// Option configures Open. Options are the extension point: an ingest hook, a
// subscriber or a metadata handler is added as one, so none of them changes
// Open's signature.
type Option func(*options)

type options struct {
	forceReindex bool
}

// WithForceReindex rebuilds the index from the store even when it looks clean.
// The drift check only compares what it can observe cheaply (see storeDrifted),
// so an operator who knows better says so this way.
func WithForceReindex() Option {
	return func(o *options) { o.forceReindex = true }
}

// Open opens the library rooted at cfg.Root, rebuilding the index from the
// store when it is missing, stale, or WithForceReindex is passed.
func Open(cfg Config, opts ...Option) (*Library, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}

	if err := os.MkdirAll(cfg.Root, 0755); err != nil {
		return nil, fmt.Errorf("creating library root: %w", err)
	}
	if err := os.MkdirAll(cfg.InboxTemp, 0700); err != nil {
		return nil, fmt.Errorf("creating inbox temp dir: %w", err)
	}
	if err := cleanInboxTemp(cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("cleaning inbox temp: %w", err)
	}
	if err := checkSameFilesystem(cfg.Root, cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("inbox_temp must be on the same filesystem as library.root: %w", err)
	}

	idx, err := index.Open(cfg.IndexPath)
	if err != nil {
		return nil, err
	}
	lib := &Library{
		store:     store.New(cfg.Root),
		index:     idx,
		inboxTemp: cfg.InboxTemp,
	}
	// Spelled out rather than as one || chain (which short-circuits the same
	// way) so the store scan can be captured: when storeDrifted is the check
	// that fires, its scan is handed to the rebuild, which then neither walks
	// the store nor stats the books a second time.
	var onDisk *storeScan
	needs := o.forceReindex || lib.needsReindex()
	if !needs {
		onDisk, needs = lib.storeDrifted()
	}
	if needs {
		if err := lib.reindex(onDisk); err != nil {
			// The index was opened above and lib is never returned, so nothing
			// else will ever close it. A duplicate book id makes this a routine
			// path (see DECISIONS.md #14), not just a crash-adjacent one.
			idx.Close()
			return nil, fmt.Errorf("reindexing library: %w", err)
		}
	} else {
		slog.Info("reindex: index is clean, skipping")
	}
	return lib, nil
}

func cleanInboxTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !e.Type().IsRegular() || !strings.HasSuffix(e.Name(), ".epub") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if err := os.Remove(path); err != nil {
			slog.Warn("removing stale inbox temp failed", "path", path, "error", err)
		} else {
			slog.Info("removed stale inbox temp", "path", path)
		}
	}
	return nil
}

// checkSameFilesystem verifies that a and b are on the same mount, which ingest
// relies on: the frontend writes to a temp file inside inboxTemp then atomically
// renames it into the library root, and rename only works within a filesystem.
func checkSameFilesystem(a, b string) error {
	tmp, err := os.CreateTemp(a, ".fschk-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	dst := filepath.Join(b, filepath.Base(tmp.Name()))
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(dst)
		return err
	}
	if err := os.Remove(dst); err != nil {
		slog.Warn("checkSameFilesystem cleanup failed", "path", dst, "error", err)
	}
	return nil
}
