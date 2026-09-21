package library

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

// Option is the extension point, so a new one does not change Open's
// signature.
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
	// Spelled out rather than as one || chain, which short-circuits the same
	// way, so the store scan can be captured. When storeDrifted is the check
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
			// path (DECISIONS.md #14).
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

// checkSameFilesystem tries a real rename, which is the only way to answer the
// question ingest turns on. Config says why the two must share a filesystem.
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
