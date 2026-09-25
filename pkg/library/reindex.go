package library

import (
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

// Reindex rebuilds the index from the store. A book that cannot be read is
// logged and skipped rather than failing the rebuild.
//
// Open calls reindex directly, since nothing else holds the library yet.
func (l *Library) Reindex() error {
	l.mutateMu.Lock()
	defer l.mutateMu.Unlock()
	return l.reindex(nil)
}

// reindex takes storeDrifted's scan when there is one, which saves a walk and
// a stat per book at startup. With nil it walks the store itself.
func (l *Library) reindex(scan *storeScan) error {
	if scan == nil {
		entries, err := l.store.Walk()
		if err != nil {
			return err
		}
		scan = &storeScan{entries: entries}
	}

	// Stat before the moves: observations are keyed by the pre-move path, and
	// rename preserves size and mtime, so they stay accurate after.
	s := l.scanEntries(scan)
	// Before the moves, so the reported paths are the ones on disk.
	if err := s.checkDuplicateIDs(); err != nil {
		return err
	}
	l.moveToCanonical(s.indexed)

	if err := l.index.Rebuild(s.indexed, s.unindexed, s.maxID); err != nil {
		return err
	}
	slog.Info("reindex: indexed books", "indexed", len(s.indexed), "total", len(scan.entries))
	return nil
}

type storeScan struct {
	entries []book.Location
	info    map[string]drift.PathInfo
}

// storeDrifted reports whether a book changed on disk behind the library. It
// compares each file's size and mtime with the index, without parsing (see
// drift.PathInfo for why size too).
//
// It returns its scan for reindex to reuse. A nil scan means the walk failed,
// and reindex walks again and reports the error.
func (l *Library) storeDrifted() (*storeScan, bool) {
	entries, err := l.store.Walk()
	if err != nil {
		slog.Warn("reindex: could not walk store, forcing rebuild", "error", err)
		return nil, true
	}

	onDisk := &storeScan{entries: entries, info: make(map[string]drift.PathInfo, len(entries))}
	for _, e := range entries {
		mt, err := l.store.Stat(e)
		if err != nil {
			// Recorded as unobserved, the same marker the index holds.
			slog.Warn("reindex: could not stat, recording as unreadable", "path", e.Dir(), "error", err)
			mt = drift.PathInfo{}
		}
		onDisk.info[e.EpubPath] = mt
	}

	indexed, err := l.index.AllPathInfo()
	if err != nil {
		slog.Warn("reindex: could not read indexed path info, forcing rebuild", "error", err)
		return onDisk, true
	}

	if len(onDisk.info) != len(indexed) {
		return onDisk, true
	}
	for path, mt := range onDisk.info {
		if im, ok := indexed[path]; !ok || !im.Equal(mt) {
			return onDisk, true
		}
	}
	return onDisk, false
}

func (l *Library) scanEntries(scan *storeScan) *scanState {
	s := &scanState{
		indexed:   make([]index.BookPath, 0, len(scan.entries)),
		unindexed: make(map[string]drift.PathInfo),
	}
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, runtime.GOMAXPROCS(0))
	)
	for _, e := range scan.entries {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			l.scanEntry(s, scan.info, e)
		}()
	}
	wg.Wait()
	return s
}

// scanEntry records one book directory in s: indexed when its sidecar and epub
// both read, skipped otherwise. Either way it reserves the directory's id.
func (l *Library) scanEntry(s *scanState, known map[string]drift.PathInfo, e book.Location) {
	// The stat error is held, so an unstattable book still reserves its id.
	pi, statErr := l.pathInfo(known, e)
	if statErr != nil {
		pi = drift.PathInfo{}
	}

	meta, err := l.store.ReadMeta(e)
	if err != nil {
		slog.Warn("reindex: skip, read meta failed", "path", e.Dir(), "error", err)
		// The id is also in the directory name. Reissuing it would make the
		// repaired sidecar a fatal collision (docs/DECISIONS.md #14).
		if id, ok := store.IDFromPath(e.Dir()); ok {
			s.reserveID(id)
		}
		s.skip(e.EpubPath, pi)
		return
	}
	// Reserved even if the stat failed or the epub will not parse.
	s.reserveID(meta.ID)

	if statErr != nil {
		slog.Warn("reindex: skip, stat failed", "path", e.Dir(), "error", statErr)
		s.skip(e.EpubPath, pi)
		return
	}

	bib, err := epub.Parse(l.store.AbsPath(e.EpubPath))
	if err != nil {
		slog.Warn("reindex: skip, parse epub failed", "path", e.Dir(), "error", err)
		s.skip(e.EpubPath, pi)
		return
	}

	b := bookFromBib(*bib, *meta, e, pi)
	s.add(index.BookPath{Book: b, Info: pi})
}

// moveToCanonical moves books to their canonical paths. A book that cannot
// move stays put and stays indexed. It updates the books in indexed, so
// Rebuild records where they landed.
func (l *Library) moveToCanonical(indexed []index.BookPath) {
	for _, bp := range indexed {
		b := bp.Book
		canonical := l.store.Layout(b.Authors, b.Title, b.Meta.ID)
		if canonical.Dir() != b.Dir() || canonical.Filename() != b.Filename() {
			if err := l.store.Move(b.Location, canonical); err != nil {
				slog.Warn("reindex: move to canonical location failed", "from", b.Dir(), "to", canonical.Dir(), "error", err)
				continue
			}
			b.Location = canonical
		}
	}
}

// pathInfo prefers storeDrifted's observation over a fresh stat, except an
// unobserved one, which recorded a failure rather than a reading.
func (l *Library) pathInfo(known map[string]drift.PathInfo, loc book.Location) (drift.PathInfo, error) {
	if pi, ok := known[loc.EpubPath]; ok && !pi.IsUnobserved() {
		return pi, nil
	}
	return l.store.Stat(loc)
}

func (l *Library) needsReindex() bool {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		slog.Warn("reindex: could not check index state, forcing rebuild", "error", err)
		return true
	}
	return needs
}

// scanState is written concurrently, only through its methods, which hold mu.
type scanState struct {
	mu        sync.Mutex
	indexed   []index.BookPath
	unindexed map[string]drift.PathInfo
	maxID     int64
}

func (s *scanState) add(bp index.BookPath) {
	s.mu.Lock()
	s.indexed = append(s.indexed, bp)
	s.mu.Unlock()
}

// skip records a directory this rebuild cannot index, so drift detection does
// not take it for one that appeared unaccounted for.
func (s *scanState) skip(path string, pi drift.PathInfo) {
	s.mu.Lock()
	s.unindexed[path] = pi
	s.mu.Unlock()
}

func (s *scanState) reserveID(id int64) {
	s.mu.Lock()
	if id > s.maxID {
		s.maxID = id
	}
	s.mu.Unlock()
}

// checkDuplicateIDs fails the rebuild when two directories claim one id, as a
// copied directory or a restored backup does. That is fatal
// (docs/DECISIONS.md #14).
//
// It is caught here rather than by the primary key because SQLite's error
// names neither directory. Sorting makes the reported pair stable.
func (s *scanState) checkDuplicateIDs() error {
	slices.SortFunc(s.indexed, func(a, b index.BookPath) int {
		return strings.Compare(a.Book.EpubPath, b.Book.EpubPath)
	})
	owners := make(map[int64]string, len(s.indexed))
	for _, bp := range s.indexed {
		id := bp.Book.Meta.ID
		if owner, dup := owners[id]; dup {
			return fmt.Errorf("duplicate book id %d claimed by %q and %q: "+
				"remove one directory, or change its id in meta.toml, then restart",
				id, owner, bp.Book.EpubPath)
		}
		owners[id] = bp.Book.EpubPath
	}
	return nil
}
