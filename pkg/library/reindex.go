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

// Reindex unconditionally rebuilds the index from the store (the source of
// truth). Books that can't be read are logged and skipped rather than failing
// the whole rebuild.
//
// Open rebuilds through reindex directly rather than through here: nothing else
// holds a reference to the library yet, and mutateMu is there for the callers
// that do.
func (l *Library) Reindex() error {
	l.mutateMu.Lock()
	defer l.mutateMu.Unlock()
	return l.reindex(nil)
}

// reindex rebuilds the index. scan, when non-nil, is the traversal storeDrifted
// already performed. Reusing it saves both the walk and a stat per book on the
// startup path that most often reaches here, where the scan happened moments ago.
// Without one, the store is walked here and the scan carries no observations,
// so every book is stat'd during the scan below.
func (l *Library) reindex(scan *storeScan) error {
	if scan == nil {
		entries, err := l.store.Walk()
		if err != nil {
			return err
		}
		scan = &storeScan{entries: entries}
	}

	// Each entry is stat'd during the scan, before the canonical moves below.
	// That ordering is what makes reusing the observations safe. They are keyed
	// by pre-move library path, so reading them there lines the keys up and stops
	// a book that moves into a path another just vacated from picking up that
	// book's state. Rename preserves size and mtime, so a value captured then
	// stays accurate once the moves run.
	s := l.scanEntries(scan)
	// Before the moves, so the reported paths are the ones on disk right now.
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

// storeScan is one traversal of the store: every book directory the walk found,
// and the file state observed for each, keyed by library path. Both halves are
// carried together so a reindex triggered by a drift verdict can reuse the whole
// traversal rather than walking and stat'ing the library a second time.
type storeScan struct {
	entries []book.Location
	info    map[string]drift.PathInfo
}

// storeDrifted reports whether something bypassed the library and added,
// removed or swapped a book on disk. It compares a directory listing against
// the index on each file's size and mtime, with no parse, both from the stat
// that recorded them. Size as well as mtime, because coarse-clock filesystems
// reuse an mtime for writes in the same tick (see drift.PathInfo).
//
// It returns the scan it built, so a reindex triggered by that verdict reuses
// it. A nil scan means the walk failed; reindex then walks and stats everything
// and surfaces the walk error rather than swallowing it here.
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
			// Unobserved rather than a forced rebuild, so both sides record the
			// same marker (see drift.PathInfo).
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

// scanEntries runs a bounded worker pool, since each entry is independent disk
// and CPU work and reindex blocks startup.
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
// both read, skipped with whatever file state was observed when they don't.
// Either way it reserves the id the directory holds.
func (l *Library) scanEntry(s *scanState, known map[string]drift.PathInfo, e book.Location) {
	// One stat up front serves every branch below. The error is held rather
	// than acted on, so an unstattable book still reserves its id.
	pi, statErr := l.pathInfo(known, e)
	if statErr != nil {
		pi = drift.PathInfo{}
	}

	meta, err := l.store.ReadMeta(e)
	if err != nil {
		slog.Warn("reindex: skip, read meta failed", "path", e.Dir(), "error", err)
		// The sidecar is unreadable, but the layout encodes the id in the
		// directory name. Reserve it anyway: this book still holds that id, and
		// reissuing it would collide the moment the sidecar is repaired: a
		// collision that now refuses to start.
		if id, ok := store.IDFromPath(e.Dir()); ok {
			s.reserveID(id)
		}
		s.skip(e.EpubPath, pi)
		return
	}
	// Reserve as soon as the meta is readable, even if the epub then fails to
	// parse or the stat failed. The id is taken and must not be reissued.
	s.reserveID(meta.ID)

	// No trustworthy file state to index against.
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

// moveToCanonical migrates books to the canonical naming convention (e.g.
// all-author directory and filename). Books that can't be moved stay at their
// old location, and the index still tracks them. This mutates the *book.Book
// values indexed holds, so Rebuild writes each book's post-move location
// against the file state the scan captured.
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

// pathInfo returns loc's on-disk file state, preferring an entry already
// captured by storeDrifted's scan over a fresh stat. An unobserved entry
// records a failure, so it is re-stat'd. Handing it back as a reading would
// index the book against file state nobody saw.
func (l *Library) pathInfo(known map[string]drift.PathInfo, loc book.Location) (drift.PathInfo, error) {
	if pi, ok := known[loc.EpubPath]; ok && !pi.IsUnobserved() {
		return pi, nil
	}
	return l.store.Stat(loc)
}

// needsReindex forces a rebuild when it cannot tell.
func (l *Library) needsReindex() bool {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		slog.Warn("reindex: could not check index state, forcing rebuild", "error", err)
		return true
	}
	return needs
}

// scanState accumulates one rebuild's view of the store. Entries are scanned
// concurrently, so every field is written through the methods below, which hold
// mu; the scan itself never locks.
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

// skip records a directory this rebuild can't index, so drift detection can
// tell it apart from one that appeared on disk unaccounted for.
func (s *scanState) skip(path string, pi drift.PathInfo) {
	s.mu.Lock()
	s.unindexed[path] = pi
	s.mu.Unlock()
}

// reserveID marks id as taken, so a later ingest cannot reissue it.
func (s *scanState) reserveID(id int64) {
	s.mu.Lock()
	if id > s.maxID {
		s.maxID = id
	}
	s.mu.Unlock()
}

// checkDuplicateIDs fails the rebuild when two directories claim one id: a
// copied book directory or a restored backup beside the original.
//
// Fatal by design (docs/DECISIONS.md #14), since renumbering breaks external
// references keyed on the id and dropping one hides a library quietly missing
// a book.
//
// Caught here rather than by the books primary key purely for the message:
// SQLite reports only "UNIQUE constraint failed: books.id" and names neither
// directory. Sorting first makes the reported pair stable, since the scan's
// workers finish in arbitrary order.
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
