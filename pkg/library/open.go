package library

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/reconcile"
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

	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
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
	if err := prepareInboxTemp(cfg.InboxTemp, cfg.Root); err != nil {
		return nil, err
	}

	if cfg.StateDir == "" {
		cfg.StateDir = cfg.Root
	}

	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.IndexPath), 0o755); err != nil {
		return nil, fmt.Errorf("creating index dir: %w", err)
	}

	idx, err := index.Open(cfg.IndexPath, o.allowSMB)
	if err != nil {
		return nil, err
	}

	lib := &Library{
		store:     store.New(cfg.Root),
		index:     idx,
		stateDir:  cfg.StateDir,
		inboxTemp: cfg.InboxTemp,
		lockFile:  lockFile,
	}
	if err := lib.store.RemoveStaleTemps(); err != nil {
		slog.Warn("cleaning stale temp files failed", "error", err)
	}

	if err := lib.store.EmptyTrash(); err != nil {
		slog.Warn("emptying trash failed", "error", err)
	}

	if err := reconcile.Run(lib.store, idx, o.forceReindex); err != nil {
		// lib is never returned, so nothing else would close the index. A
		// duplicate book id makes this a routine path (docs/DECISIONS.md #14).
		idx.Close()
		return nil, fmt.Errorf("reindexing library: %w", err)
	}

	recordLastID(lib.store, idx)

	return lib, nil
}

// recordLastID brings the store's record of the highest issued id up to the
// index's. A rebuild seeds the sequence past every id on disk, so after any
// Open the index's figure is the higher one wherever the record missed an id:
// a library from before the record, or a book copied in by hand.
//
// A failure only risks reissuing a deleted id later, so it is logged. An
// unreadable record is overwritten, since the index's figure already covers
// every id in use.
func recordLastID(s *store.Store, idx *index.Index) {
	issued, err := idx.LastIssuedID()
	if err != nil {
		slog.Warn("could not read the last issued book id from the index", "error", err)
		return
	}

	last, err := s.LastID()
	if err != nil {
		slog.Warn("replacing an unreadable record of the last issued book id", "error", err)
	}

	if issued > last {
		if err := s.RecordID(issued); err != nil {
			slog.Warn("could not record the last issued book id; a deleted book's id may be reissued", "error", err)
		}
	}
}

func prepareInboxTemp(dir, root string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating inbox temp dir: %w", err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		return fmt.Errorf("cleaning inbox temp: %w", err)
	}

	if err := checkSameFilesystem(root, dir); err != nil {
		return fmt.Errorf("inbox_temp must be on the same filesystem as library.root: %w", err)
	}

	return nil
}

func cleanInboxTemp(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}

	for _, e := range entries {
		upload := e.Type().IsRegular() && strings.HasSuffix(e.Name(), ".epub")
		scratch := e.IsDir() && strings.HasPrefix(e.Name(), "hooks-")

		if !upload && !scratch {
			continue
		}

		path := filepath.Join(dir, e.Name())
		if err := os.RemoveAll(path); err != nil {
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
