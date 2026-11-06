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

type Option func(*options)

type options struct {
	forceReindex bool
	allowSMB     bool
}

// WithForceReindex rebuilds the index even when it looks clean. The drift check
// compares only size and mtime, so it can miss a change.
func WithForceReindex() Option {
	return func(o *options) { o.forceReindex = true }
}

// WithAllowSMB permits opening on SMB/CIFS filesystems, which have unreliable
// atomic rename and file locking. Use only if you understand the risks.
func WithAllowSMB() Option {
	return func(o *options) { o.allowSMB = true }
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

	if err := checkFilesystem(cfg.Root, o.allowSMB); err != nil {
		return nil, err
	}

	lockFile, err := acquireLock(cfg.Root)
	if err != nil {
		return nil, err
	}

	lib, err := openLocked(cfg, o, lockFile)
	if err != nil {
		lockFile.Close()
		return nil, err
	}

	return lib, nil
}

// openLocked does the work of opening the library while holding the lock.
// If it fails, the caller is responsible for closing lockFile.
func openLocked(cfg Config, o options, lockFile *os.File) (*Library, error) {
	if err := os.MkdirAll(cfg.InboxTemp, 0700); err != nil {
		return nil, fmt.Errorf("creating inbox temp dir: %w", err)
	}
	if err := cleanInboxTemp(cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("cleaning inbox temp: %w", err)
	}
	if err := cleanStaleTmpFiles(cfg.Root); err != nil {
		slog.Warn("cleaning stale epub temp files failed", "error", err)
	}
	if err := checkSameFilesystem(cfg.Root, cfg.InboxTemp); err != nil {
		return nil, fmt.Errorf("inbox_temp must be on the same filesystem as library.root: %w", err)
	}

	idx, err := index.Open(cfg.IndexPath, o.allowSMB)
	if err != nil {
		return nil, err
	}
	lib := &Library{
		store:     store.New(cfg.Root),
		index:     idx,
		root:      cfg.Root,
		stateDir:  cfg.StateDir,
		inboxTemp: cfg.InboxTemp,
		lockFile:  lockFile,
	}

	// Not one || chain, so storeDrifted's scan can be handed to reindex.
	var onDisk *storeScan
	needs := o.forceReindex || lib.needsReindex()
	if !needs {
		onDisk, needs = lib.storeDrifted()
	}
	if needs {
		if err := lib.reindex(onDisk); err != nil {
			// lib is never returned, so nothing else would close the index. A
			// duplicate book id makes this a routine path (docs/DECISIONS.md #14).
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

// checkSameFilesystem tries a real rename, the only reliable test of what ingest needs.
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
