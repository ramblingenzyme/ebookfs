package library

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ErrBookNotFound is wrapped into the error returned by any operation that
// addresses a book id the index does not hold. Callers distinguish it with
// errors.Is; anything else from the same call is an index or store failure.
var ErrBookNotFound = errors.New("no such book")

// Library defines the public API for filesystem and index operations on the
// book collection. The concrete implementation is unexported; construct via New.
//
// Concurrency contract: methods are safe for concurrent use. Search returns
// *model.Book values that are immutable snapshots — the library never mutates a
// Book after returning it. Every other operation addresses a book by id and
// resolves its current state fresh, so callers never pass stale snapshots back
// in: Content opens the book's live on-disk file, and mutations (Edit, Delete)
// run as an atomic read-modify-write per book under a per-book lock, so callers
// cannot revert other callers' changes.
type Library interface {
	Close() error
	CreateIngest() (IngestHandle, error)
	// Exporter creates a view of the library for export (reader/ view). Any
	// resources it holds are released by Library.Close — the caller has no
	// teardown to perform, which is why Exporter has no Close method.
	Exporter(config.ReaderConfig) (Exporter, error)
	Search(model.Query) ([]*model.Book, error)
	Stats() (*model.Stats, error)
	Reindex() error
	// Content returns an open handle to the book's epub content. The caller
	// must close it. The handle reflects the book at the time of the call;
	// after a concurrent Edit, call Content again to read updated content.
	// The returned reader is non-nil iff err is nil.
	Content(id int64) (model.EpubReader, error)
	Edit(id int64, e model.Edits) (*model.Book, error)
	Delete(id int64) error
}

// Exporter produces the rsync-export rendition of a book for the reader/ view.
// It is the single swap point between serving the original epub and a converted
// kepub: the Library returns the appropriate implementation based on config.
//
// Includes (which books belong in the reader) and Dirname (how they group) sit
// here alongside the rendition methods on purpose: deciding *what* syncs to the
// reader is a library/backend policy concern, and fs/views/reader.go is only the
// concrete 9P rendering of that data. Includes is a predicate rather than an
// exposed status list so the policy can change (tag-based, size caps, …)
// without touching the frontend.
type Exporter interface {
	// Open returns a handle to the book's export rendition. The handle is a
	// snapshot — after the book is edited, call Open again for updated content.
	// The returned reader is non-nil iff err is nil.
	Open(*model.Book) (model.EpubReader, error)
	Size(*model.Book) (int64, bool) // cheap; 9P stat length, false when cold
	Warm(*model.Book)               // non-blocking proactive warm hint
	Filename(*model.Book) string    // FAT-safe export name
	Dirname(*model.Book) string     // FAT-safe export directory name
	Includes(*model.Book) bool      // whether the book appears in the reader view
}

func Open(cfg config.LibraryConfig, forceReindex bool) (Library, error) {
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
	lib := &libraryImpl{
		store:     store.New(cfg.Root, cfg.InboxTemp),
		index:     idx,
		inboxTemp: cfg.InboxTemp,
	}
	// Spelled out rather than as one || chain (which short-circuits the same
	// way) so the store scan can be captured: when storeDrifted is the check
	// that fires, its scan is handed to the rebuild, which then neither walks
	// the store nor stats the books a second time.
	var onDisk *storeScan
	needs := forceReindex || lib.needsReindex()
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
