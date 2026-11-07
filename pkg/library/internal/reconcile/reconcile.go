// Package reconcile decides whether the store and index disagree, then owns
// the scan and rebuild that bring the index back in line.
package reconcile

import (
	"fmt"
	"log/slog"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

// Run reconciles the index with the store. force skips the drift check and
// rebuilds unconditionally.
func Run(files *store.Store, idx *index.Index, force bool) error {
	var scan *storeScan
	needs := force || needsReindex(idx)
	if !needs {
		scan, needs = storeDrifted(files, idx)
	}
	if !needs {
		slog.Info("reindex: index is clean, skipping")
		return nil
	}
	return reindex(files, idx, scan)
}

func reindex(files *store.Store, idx *index.Index, scan *storeScan) error {
	walkStart := time.Now()
	if scan == nil {
		entries, err := files.Walk()
		if err != nil {
			return err
		}
		scan = &storeScan{entries: entries}
	}
	walkDur := time.Since(walkStart)

	// Record observations before moves; rename preserves size and mtime, while
	// the scan keys each observation by its original path.
	scanStart := time.Now()
	s := scanEntries(files, scan)
	scanDur := time.Since(scanStart)

	dedupStart := time.Now()
	if err := s.checkDuplicateIDs(); err != nil {
		return err
	}
	dedupDur := time.Since(dedupStart)

	moveStart := time.Now()
	moveToCanonical(files, s.indexed)
	moveDur := time.Since(moveStart)

	rebuildStart := time.Now()
	if err := idx.Rebuild(s.indexed, s.unindexed, s.maxID); err != nil {
		return err
	}
	rebuildDur := time.Since(rebuildStart)

	slog.Info("reindex: indexed books",
		"indexed", len(s.indexed), "total", len(scan.entries),
		"walk", walkDur, "scan", scanDur, "dedup", dedupDur, "move", moveDur, "rebuild", rebuildDur,
	)
	return nil
}

type storeScan struct {
	entries []book.Location
	info    map[string]drift.PathInfo
}

// storeDrifted compares path, size, and mtimes without parsing the epubs. It
// returns the walk for reindex to reuse when a rebuild is needed.
func storeDrifted(files *store.Store, idx *index.Index) (*storeScan, bool) {
	entries, err := files.Walk()
	if err != nil {
		slog.Warn("reindex: could not walk store, forcing rebuild", "error", err)
		return nil, true
	}

	onDisk := &storeScan{entries: entries, info: make(map[string]drift.PathInfo, len(entries))}
	for _, e := range entries {
		mt, err := files.Stat(e)
		if err != nil {
			slog.Warn("reindex: could not stat, recording as unreadable", "path", e.Dir(), "error", err)
			mt = drift.PathInfo{}
		}
		onDisk.info[e.EpubPath] = mt
	}

	indexed, err := idx.AllPathInfo()
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

func scanEntries(files *store.Store, scan *storeScan) *scanState {
	s := &scanState{
		indexed:   make([]index.BookPath, 0, len(scan.entries)),
		unindexed: make(map[string]drift.PathInfo),
	}
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, runtime.GOMAXPROCS(0))
	)
	start := time.Now()
	for _, e := range scan.entries {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			scanEntry(files, s, scan.info, e)
		}()
	}
	wg.Wait()
	slog.Info("reindex: scan",
		"wall", time.Since(start),
		"stat", s.statDur,
		"meta", s.metaDur,
		"parse", s.parseDur,
		"books", len(s.indexed),
	)
	return s
}

func scanEntry(files *store.Store, s *scanState, known map[string]drift.PathInfo, e book.Location) {
	start := time.Now()
	pi, statErr := pathInfo(files, known, e)
	statDur := time.Since(start)
	if statErr != nil {
		pi = drift.PathInfo{}
	}

	start = time.Now()
	meta, err := files.ReadMeta(e)
	metaDur := time.Since(start)
	if err != nil {
		slog.Warn("reindex: skip, read meta failed", "path", e.Dir(), "error", err)
		// The directory suffix can still reserve an id the sidecar cannot expose.
		if id, ok := store.IDFromPath(e.Dir()); ok {
			s.reserveID(id)
		}
		s.skip(e.EpubPath, pi)
		s.recordDurations(statDur, metaDur, 0)
		return
	}
	// Reserve the id even if the epub cannot be observed or parsed.
	s.reserveID(meta.ID)

	if statErr != nil {
		slog.Warn("reindex: skip, stat failed", "path", e.Dir(), "error", statErr)
		s.skip(e.EpubPath, pi)
		s.recordDurations(statDur, metaDur, 0)
		return
	}

	start = time.Now()
	bib, err := epub.Parse(files.AbsPath(e.EpubPath))
	parseDur := time.Since(start)
	if err != nil {
		slog.Warn("reindex: skip, parse epub failed", "path", e.Dir(), "error", err)
		s.skip(e.EpubPath, pi)
		s.recordDurations(statDur, metaDur, parseDur)
		return
	}

	b := book.NewBook(*bib, *meta, e)
	b.EpubSize = pi.Size
	s.add(index.BookPath{Book: b, Info: pi})
	s.recordDurations(statDur, metaDur, parseDur)
}

func moveToCanonical(files *store.Store, indexed []index.BookPath) {
	for _, bp := range indexed {
		b := bp.Book
		canonical := files.Layout(b.Authors, b.Title, b.Meta.ID)
		if canonical.Dir() != b.Dir() || canonical.Filename() != b.Filename() {
			if err := files.Move(b.Location, canonical); err != nil {
				slog.Warn("reindex: move to canonical location failed", "from", b.Dir(), "to", canonical.Dir(), "error", err)
				continue
			}
			b.Location = canonical
		}
	}
}

// pathInfo reuses a successful drift-check observation, but retries an
// unobserved path in case the file became readable before scanning.
func pathInfo(files *store.Store, known map[string]drift.PathInfo, loc book.Location) (drift.PathInfo, error) {
	if pi, ok := known[loc.EpubPath]; ok && !pi.IsUnobserved() {
		return pi, nil
	}
	return files.Stat(loc)
}

func needsReindex(idx *index.Index) bool {
	needs, err := idx.NeedsReindex()
	if err != nil {
		slog.Warn("reindex: could not check index state, forcing rebuild", "error", err)
		return true
	}
	return needs
}

// scanState is written concurrently only through methods that hold mu.
type scanState struct {
	mu        sync.Mutex
	indexed   []index.BookPath
	unindexed map[string]drift.PathInfo
	maxID     int64
	statDur   time.Duration
	metaDur   time.Duration
	parseDur  time.Duration
}

func (s *scanState) add(bp index.BookPath) {
	s.mu.Lock()
	s.indexed = append(s.indexed, bp)
	s.mu.Unlock()
}

// skip records a directory the rebuild cannot index, so the next drift check
// does not treat it as an unaccounted-for book.
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

func (s *scanState) recordDurations(stat, meta, parse time.Duration) {
	s.mu.Lock()
	s.statDur += stat
	s.metaDur += meta
	s.parseDur += parse
	s.mu.Unlock()
}

// checkDuplicateIDs reports both paths because SQLite's primary-key error
// names neither directory. Sorting keeps the reported pair stable.
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
