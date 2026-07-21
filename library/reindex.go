package library

import (
	"fmt"
	"log"
	"runtime"
	"slices"
	"strings"
	"sync"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// scanState accumulates one rebuild's view of the store. Entries are scanned
// concurrently, so every field is written through the methods below, which hold
// mu — the scan itself never locks.
type scanState struct {
	mu        sync.Mutex
	indexed   []index.BookPath
	unindexed map[string]drift.PathInfo
	maxID     int64
}

// add records a book this rebuild indexed.
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

// storeDrifted reports whether the on-disk store no longer matches the index
// — a book directory added or removed, or a file swapped out — by something
// that bypassed the library (e.g. a manual edit to the store).
// It compares a directory listing against the index using each file's size and
// modification time (cheap, no parse), both taken from the same stat that
// recorded them. Size is compared as well as mtime because coarse-clock
// filesystems reuse an mtime for writes in the same tick (see drift.PathInfo).
// It also returns the store scan it built, keyed by library path, so a reindex
// triggered by that verdict can reuse it instead of stat'ing every book again.
// The scan is nil when the walk itself failed; reindex then stats everything.
func (l *libraryImpl) storeDrifted() (map[string]drift.PathInfo, bool) {
	entries, err := l.store.Walk()
	if err != nil {
		log.Printf("reindex: could not walk store (%v), forcing rebuild", err)
		return nil, true
	}

	onDisk := make(map[string]drift.PathInfo, len(entries))
	for _, e := range entries {
		mt, err := l.store.Stat(e)
		if err != nil {
			// Recorded as unobserved rather than forcing a rebuild: the rebuild
			// records the same marker, so the two agree and one unreadable book
			// stops meaning a full reindex on every startup (see drift.Unobserved).
			log.Printf("reindex: could not stat %s (%v), recording as unreadable", e.LibraryPath, err)
			mt = drift.Unobserved(e.EpubFilename)
		}
		onDisk[e.LibraryPath] = mt
	}

	indexed, err := l.index.AllPathInfo()
	if err != nil {
		log.Printf("reindex: could not read indexed path info (%v), forcing rebuild", err)
		return onDisk, true
	}

	if len(onDisk) != len(indexed) {
		return onDisk, true
	}
	for path, mt := range onDisk {
		if im, ok := indexed[path]; !ok || !im.Equal(mt) {
			return onDisk, true
		}
	}
	return onDisk, false
}

// Reindex unconditionally rebuilds the index from the store (the source of
// truth). Books that can't be read are logged and skipped rather than failing
// the whole rebuild.
func (l *libraryImpl) Reindex() error { return l.reindex(nil) }

// reindex rebuilds the index. known, when non-nil, is the store scan storeDrifted
// already performed — reusing it saves re-stating every book on the startup path
// that most often reaches here, where the scan happened moments earlier.
func (l *libraryImpl) reindex(known map[string]drift.PathInfo) error {
	entries, err := l.store.Walk()
	if err != nil {
		return err
	}

	// Each entry is stat'd during the scan, before the canonical moves below.
	// That ordering is what makes reusing known safe: it is keyed by pre-move
	// library path, so reading it there lines the keys up and stops a book that
	// moves into a path another just vacated from picking up that book's state.
	// Rename preserves size and mtime, so a value captured then stays accurate
	// once the moves run.
	s := l.scanEntries(entries, known)
	// Before the moves, so the reported paths are the ones on disk right now.
	if err := s.checkDuplicateIDs(); err != nil {
		return err
	}
	l.moveToCanonical(s.indexed)

	if err := l.index.Rebuild(s.indexed, s.unindexed, s.maxID); err != nil {
		return err
	}
	log.Printf("reindex: indexed %d of %d books", len(s.indexed), len(entries))
	return nil
}

// scanEntries reads every entry into a scanState on a bounded worker pool: each
// is independent disk and CPU work, and reindex blocks startup, so a large
// library would otherwise pay for every epub sequentially.
func (l *libraryImpl) scanEntries(entries []model.Location, known map[string]drift.PathInfo) *scanState {
	s := &scanState{
		indexed:   make([]index.BookPath, 0, len(entries)),
		unindexed: make(map[string]drift.PathInfo),
	}
	var (
		wg  sync.WaitGroup
		sem = make(chan struct{}, runtime.GOMAXPROCS(0))
	)
	for _, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			l.scanEntry(s, known, e)
		}()
	}
	wg.Wait()
	return s
}

// scanEntry records one book directory in s: indexed when its sidecar and epub
// both read, skipped with whatever file state was observed when they don't.
// Either way it reserves the id the directory holds.
func (l *libraryImpl) scanEntry(s *scanState, known map[string]drift.PathInfo, e model.Location) {
	// One stat up front serves every branch below, indexed or not: a directory
	// this rebuild can't index still needs its state recorded, or drift
	// detection cannot tell it from one that appeared on disk unaccounted for.
	// The error is held rather than acted on so an unstattable book still
	// reserves its id below.
	pi, statErr := l.pathInfo(known, e)
	if statErr != nil {
		// Nothing was observed, so pi is not a reading and must not be recorded
		// as one. Substituting the marker here rather than in the branches below
		// is what guarantees every skip path records something: a directory in
		// neither books nor skipped_books reads as one that appeared on disk
		// unaccounted for, which is drift on every startup (see drift.Unobserved).
		pi = drift.Unobserved(e.EpubFilename)
	}

	meta, err := l.store.ReadMeta(e)
	if err != nil {
		log.Printf("reindex: skip %s: read meta: %v", e.LibraryPath, err)
		// The sidecar is unreadable, but the layout encodes the id in the
		// directory name. Reserve it anyway: this book still holds that id, and
		// reissuing it would collide the moment the sidecar is repaired — a
		// collision that now refuses to start.
		if id, ok := store.IDFromPath(e.LibraryPath); ok {
			s.reserveID(id)
		}
		s.skip(e.LibraryPath, pi)
		return
	}
	// Reserve as soon as the meta is readable, even if the epub then fails to
	// parse or the stat failed — the id is taken and must not be reissued.
	s.reserveID(meta.ID)

	// Left out of the rebuild — there is no trustworthy file state to index it
	// against — but recorded as unobserved so drift detection agrees with the
	// same verdict next startup instead of rebuilding forever.
	if statErr != nil {
		log.Printf("reindex: skip %s: stat: %v", e.LibraryPath, statErr)
		s.skip(e.LibraryPath, pi)
		return
	}

	book, err := epub.Parse(e.EpubPath)
	if err != nil {
		log.Printf("reindex: skip %s: parse epub: %v", e.LibraryPath, err)
		s.skip(e.LibraryPath, pi)
		return
	}

	b := bookFromBib(bibFromEpub(book), *meta, e, pi)
	s.add(index.BookPath{Book: b, Info: pi})
}

// checkDuplicateIDs fails the rebuild when two directories claim one id.
//
// That is user error — a copied book directory, or a restored backup sitting
// alongside the original — and it is fatal by design (DECISIONS.md #14):
// renumbering would break external references keyed on the id, and dropping one
// would hide the problem behind a library that looks fine but is quietly missing
// a book.
//
// Detected here rather than left to the books primary key purely for the error
// message: SQLite reports only "UNIQUE constraint failed: books.id", which
// doesn't say which directories collided. Sorting first makes the reported pair
// stable, since the scan's workers finish in arbitrary order.
func (s *scanState) checkDuplicateIDs() error {
	slices.SortFunc(s.indexed, func(a, b index.BookPath) int {
		return strings.Compare(a.Book.Location.LibraryPath, b.Book.Location.LibraryPath)
	})
	owners := make(map[int64]string, len(s.indexed))
	for _, bp := range s.indexed {
		id := bp.Book.Meta.ID
		if owner, dup := owners[id]; dup {
			return fmt.Errorf("duplicate book id %d claimed by %q and %q: "+
				"remove one directory, or change its id in meta.toml, then restart",
				id, owner, bp.Book.Location.LibraryPath)
		}
		owners[id] = bp.Book.Location.LibraryPath
	}
	return nil
}

// moveToCanonical migrates books to the canonical naming convention (e.g.
// all-author directory and filename). Books that can't be moved stay at their
// old location — the index will still track them correctly. This mutates the
// *model.Book values indexed holds, so Rebuild writes each book's post-move
// location against the file state the scan captured.
func (l *libraryImpl) moveToCanonical(indexed []index.BookPath) {
	for _, bp := range indexed {
		b := bp.Book
		canonical := l.store.Layout(b.Authors, b.Title, b.Meta.ID)
		if canonical.LibraryPath != b.Location.LibraryPath || canonical.EpubFilename != b.Location.EpubFilename {
			if err := l.store.Move(b.Location, canonical); err != nil {
				log.Printf("reindex: move %s -> %s: %v", b.Location.LibraryPath, canonical.LibraryPath, err)
				continue
			}
			b.Location = canonical
			b.EpubPath = l.store.AbsPath(canonical.LibraryPath, canonical.EpubFilename)
		}
	}
}

// pathInfo returns loc's on-disk file state, preferring an entry already
// captured by storeDrifted's scan over a fresh stat. An unobserved entry is a
// record of failure rather than a usable reading, so it is re-stat'd instead of
// being handed back as a successful one — which would index the book against
// file state that was never actually seen.
func (l *libraryImpl) pathInfo(known map[string]drift.PathInfo, loc model.Location) (drift.PathInfo, error) {
	if pi, ok := known[loc.LibraryPath]; ok && !pi.IsUnobserved() {
		return pi, nil
	}
	return l.store.Stat(loc)
}

// needsReindex reports whether the index requires a rebuild — true when there
// are pending operations or the schema version is stale.
func (l *libraryImpl) needsReindex() bool {
	needs, err := l.index.NeedsReindex()
	if err != nil {
		log.Printf("reindex: could not check index state (%v), forcing rebuild", err)
		return true
	}
	return needs
}
