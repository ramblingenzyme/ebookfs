package library

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/syncutil"
	"github.com/ramblingenzyme/ebookfs/library/config"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/library/internal/store"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

type libraryImpl struct {
	store     *store.Store
	index     *index.Index
	inboxTemp string
	exporters []Exporter
	expMu     sync.Mutex
	// Dedup of exporters by config is not implemented. If needed in the
	// future, hash/comparable-key the ReaderConfig fields and store in a map.

	// bookMu serializes the operations that mutate one book's on-disk state
	// (Edit, Delete), so e.g. a cover rewrite cannot interleave with an edit
	// that is moving the book directory.
	bookMu syncutil.KeyedMutex

	// ingestMu serializes the entire ingest path (Exists → NextID → Layout →
	// Ingest → index Put) so two simultaneous uploads of the same new book
	// cannot both pass the Exists check before either lays the book down.
	ingestMu sync.Mutex
}

// get returns the current state of book id from the index, hydrated with its
// absolute epub path. Mutations fetch their base through it under the per-book
// lock, so they always operate on the book's authoritative current state.
func (l *libraryImpl) get(id int64) (*model.Book, error) {
	b, err := l.index.Get(id)
	if err != nil {
		return nil, fmt.Errorf("no book with id %d: %w", id, err)
	}
	b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	return b, nil
}

type exporterCloser interface{ close() error }

func (l *libraryImpl) Close() error {
	l.expMu.Lock()
	for _, e := range l.exporters {
		if c, ok := e.(exporterCloser); ok {
			c.close()
		}
	}
	l.expMu.Unlock()
	return l.index.Close()
}

func (l *libraryImpl) Exporter(cfg config.ReaderConfig) (Exporter, error) {
	e, err := newExporter(cfg, l)
	if err != nil {
		return nil, err
	}
	l.expMu.Lock()
	l.exporters = append(l.exporters, e)
	l.expMu.Unlock()
	kind := "epub"
	if cfg.Convert {
		kind = "kepub"
	}
	log.Printf("export: %s for statuses %v", kind, cfg.Statuses)
	return e, nil
}

func (l *libraryImpl) CreateIngest() (IngestHandle, error) {
	f, err := os.CreateTemp(l.inboxTemp, "*.epub")
	if err != nil {
		return nil, err
	}
	return &ingestHandle{file: f, ingestFn: l.ingestPath}, nil
}

// ingestPath parses the staged epub, lays it down in the store, and records it
// in the index.
func (l *libraryImpl) ingestPath(epubPath string) (*model.Book, error) {
	// Parse before taking ingestMu: it touches only this upload's staged temp
	// file, so bulk uploads overlap their parsing instead of serializing on it.
	book, err := epub.Parse(epubPath)
	if err != nil {
		return nil, err
	}

	bib := bibFromEpub(book)
	if bib.Title == "" {
		return nil, fmt.Errorf("epub has no title")
	}
	if len(bib.Authors) == 0 {
		bib.Authors = []model.Author{{Name: model.UnknownAuthor, SortName: model.UnknownAuthor}}
	}

	l.ingestMu.Lock()
	defer l.ingestMu.Unlock()

	if l.store.Exists(bib.Authors, bib.Title) {
		return nil, fmt.Errorf("book already in library: %q", bib.Title)
	}

	id, err := l.index.NextID()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	meta := model.Meta{
		ID:           id,
		DateAdded:    now,
		DateModified: now,
	}
	loc := l.store.Layout(bib.Authors, bib.Title, id)
	b := model.NewBook(bib, meta, loc)

	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return nil, err
	}
	if err := l.store.Ingest(epubPath, b.Location, &b.Meta); err != nil {
		// Ingest failed; the pending row stays (forcing a healing reindex) and
		// we clean up the store so a retry starts fresh.
		_ = l.store.Delete(b.Location)
		return nil, err
	}
	mt, err := l.statBook(b.Location)
	if err != nil {
		_ = l.store.Delete(b.Location)
		return nil, err
	}
	if err := op.Put(b, mt); err != nil {
		_ = l.store.Delete(b.Location)
		return nil, err
	}

	log.Printf("ingest: book %d (%q) by %s", b.Meta.ID, b.Title, model.JoinAuthors(bib.Authors, ", "))
	return b, nil
}

func (l *libraryImpl) Query(f model.Filter) ([]*model.Book, error) {
	books, err := l.index.Query(f)
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

func (l *libraryImpl) Search(q model.Query) ([]*model.Book, error) {
	books, err := l.index.Search(q)
	if err != nil {
		return nil, err
	}
	for _, b := range books {
		b.EpubPath = l.store.AbsPath(b.LibraryPath, b.EpubFilename)
	}
	return books, nil
}

// Stats returns aggregate library statistics.
func (l *libraryImpl) Stats() (*model.Stats, error) {
	return l.index.Stats()
}

// Reindex unconditionally rebuilds the index from the store (the source of
// truth). Books that can't be read are logged and skipped rather than failing
// the whole rebuild.
func (l *libraryImpl) Reindex() error { return l.reindex(nil) }

// reindex rebuilds the index. known, when non-nil, is the store scan storeDrifted
// already performed — reusing it saves re-stating every book on the startup path
// that most often reaches here, where the scan happened moments earlier.
//
// Entries are parsed on a bounded worker pool: each is independent disk and
// CPU work, and reindex blocks startup, so a large library would otherwise
// pay for every epub sequentially.
func (l *libraryImpl) reindex(known map[string]index.PathInfo) error {
	entries, err := l.store.Walk()
	if err != nil {
		return err
	}

	// Each entry is stat'd in its own worker, before the canonical moves below.
	// That ordering is what makes reusing known safe: it is keyed by pre-move
	// library path, so reading it here lines the keys up and stops a book that
	// moves into a path another just vacated from picking up that book's state.
	// Rename preserves size and mtime, so a value captured now stays accurate
	// once the moves run.
	var (
		mu        sync.Mutex
		indexed   = make([]index.BookPath, 0, len(entries))
		unindexed = make(map[string]index.PathInfo)
		maxID     int64
		wg        sync.WaitGroup
		sem       = make(chan struct{}, runtime.GOMAXPROCS(0))
	)
	// skip records a directory this rebuild can't index, so drift detection can
	// tell it apart from one that appeared on disk unaccounted for.
	skip := func(e model.Location, pi index.PathInfo) {
		mu.Lock()
		unindexed[e.LibraryPath] = pi
		mu.Unlock()
	}
	for _, e := range entries {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()

			// One stat up front serves every branch below, indexed or not: a
			// directory this rebuild can't index still needs its state recorded,
			// or drift detection cannot tell it from one that appeared on disk
			// unaccounted for. The error is held rather than acted on so an
			// unstattable book still reserves its id below.
			pi, statErr := l.pathInfo(known, e)

			meta, err := l.store.ReadMeta(e)
			if err != nil {
				log.Printf("reindex: skip %s: read meta: %v", e.LibraryPath, err)
				// The sidecar is unreadable, but the layout encodes the id in
				// the directory name. Reserve it anyway: this book still holds
				// that id, and reissuing it would collide the moment the
				// sidecar is repaired — a collision that now refuses to start.
				if id, ok := store.IDFromPath(e.LibraryPath); ok {
					mu.Lock()
					if id > maxID {
						maxID = id
					}
					mu.Unlock()
				}
				if statErr == nil {
					skip(e, pi)
				}
				return
			}
			// Bump maxID as soon as the meta is readable, even if the epub then
			// fails to parse or the stat failed — the id is taken and must not
			// be reissued.
			mu.Lock()
			if meta.ID > maxID {
				maxID = meta.ID
			}
			mu.Unlock()

			// Left out of the rebuild — there is no trustworthy file state to
			// index it against — but recorded as unobserved so drift detection
			// agrees with the same verdict next startup instead of rebuilding
			// forever (see index.Unobserved).
			if statErr != nil {
				log.Printf("reindex: skip %s: stat: %v", e.LibraryPath, statErr)
				skip(e, index.Unobserved(e.EpubFilename))
				return
			}

			book, err := epub.Parse(e.EpubPath)
			if err != nil {
				log.Printf("reindex: skip %s: parse epub: %v", e.LibraryPath, err)
				skip(e, pi)
				return
			}

			mu.Lock()
			indexed = append(indexed, index.BookPath{
				Book: model.NewBook(bibFromEpub(book), *meta, e),
				Info: pi,
			})
			mu.Unlock()
		}()
	}
	wg.Wait()

	// Two directories claiming one id is user error — a copied book directory,
	// or a restored backup sitting alongside the original — and it is fatal by
	// design (DECISIONS.md #14): renumbering would break external references
	// keyed on the id, and dropping one would hide the problem behind a library
	// that looks fine but is quietly missing a book.
	//
	// Detected here rather than left to the books primary key purely for the
	// error message: SQLite reports only "UNIQUE constraint failed: books.id",
	// which doesn't say which directories collided. Sorting first makes the
	// reported pair stable, since the workers above finish in arbitrary order.
	slices.SortFunc(indexed, func(a, b index.BookPath) int {
		return strings.Compare(a.Book.Location.LibraryPath, b.Book.Location.LibraryPath)
	})
	owners := make(map[int64]string, len(indexed))
	for _, bp := range indexed {
		id := bp.Book.Meta.ID
		if owner, dup := owners[id]; dup {
			return fmt.Errorf("duplicate book id %d claimed by %q and %q: "+
				"remove one directory, or change its id in meta.toml, then restart",
				id, owner, bp.Book.Location.LibraryPath)
		}
		owners[id] = bp.Book.Location.LibraryPath
	}

	// Migrate books to the canonical naming convention (e.g. all-author
	// directory and filename). Books that can't be moved stay at their old
	// location — the index will still track them correctly. This mutates the
	// *model.Book values indexed holds, so Rebuild writes each book's post-move
	// location against the file state captured above.
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

	if err := l.index.Rebuild(indexed, unindexed, maxID); err != nil {
		return err
	}
	log.Printf("reindex: indexed %d of %d books", len(indexed), len(entries))
	return nil
}

// pathInfo returns loc's on-disk file state, preferring an entry already
// captured by storeDrifted's scan over a fresh stat. An unobserved entry is a
// record of failure rather than a usable reading, so it is re-stat'd instead of
// being handed back as a successful one — which would index the book against
// file state that was never actually seen.
func (l *libraryImpl) pathInfo(known map[string]index.PathInfo, loc model.Location) (index.PathInfo, error) {
	if pi, ok := known[loc.LibraryPath]; ok && !pi.IsUnobserved() {
		return pi, nil
	}
	return l.statBook(loc)
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

// statBook returns the on-disk state of a book's epub and meta.toml — the epub
// size and both modification times — used by the library layer for drift
// detection. A stat failure is returned rather than defaulted away: a zero
// mtime can never match a real file, so recording one would silently force a
// full reindex on every startup thereafter.
//
// TODO: this belongs on the store, which owns on-disk layout — it is the only
// place the library reaches past the store to the filesystem, and the reason
// store.MetaPath is exported at all. Blocked on index.PathInfo living in the
// index, which store cannot import; moving it to model unblocks both. See
// ROADMAP, "observing file state belongs to the store".
func (l *libraryImpl) statBook(loc model.Location) (index.PathInfo, error) {
	epubFI, err := os.Stat(l.store.AbsPath(loc.LibraryPath, loc.EpubFilename))
	if err != nil {
		return index.PathInfo{}, err
	}
	metaFI, err := os.Stat(l.store.MetaPath(loc))
	if err != nil {
		return index.PathInfo{}, err
	}
	return index.PathInfo{
		EpubFilename: loc.EpubFilename,
		Size:         epubFI.Size(),
		EpubMtime:    epubFI.ModTime(),
		MetaSize:     metaFI.Size(),
		MetaMtime:    metaFI.ModTime(),
	}, nil
}

// storeDrifted reports whether the on-disk store no longer matches the index
// — a book directory added or removed, or a file swapped out — by something
// that bypassed the library (e.g. a manual edit to the store).
// It compares a directory listing against the index using each file's size and
// modification time (cheap, no parse), both taken from the same stat that
// recorded them. Size is compared as well as mtime because coarse-clock
// filesystems reuse an mtime for writes in the same tick (see index.PathInfo).
// It also returns the store scan it built, keyed by library path, so a reindex
// triggered by that verdict can reuse it instead of stat'ing every book again.
// The scan is nil when the walk itself failed; reindex then stats everything.
func (l *libraryImpl) storeDrifted() (map[string]index.PathInfo, bool) {
	entries, err := l.store.Walk()
	if err != nil {
		log.Printf("reindex: could not walk store (%v), forcing rebuild", err)
		return nil, true
	}

	onDisk := make(map[string]index.PathInfo, len(entries))
	for _, e := range entries {
		mt, err := l.statBook(e)
		if err != nil {
			// Recorded as unobserved rather than forcing a rebuild: the rebuild
			// records the same marker, so the two agree and one unreadable book
			// stops meaning a full reindex on every startup (see index.Unobserved).
			log.Printf("reindex: could not stat %s (%v), recording as unreadable", e.LibraryPath, err)
			mt = index.Unobserved(e.EpubFilename)
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

// OpenEpub returns a handle to the epub content of book id. The caller must
// close it. The book's current location is resolved fresh, so the handle always
// tracks the live file even if a concurrent edit moved it.
func (l *libraryImpl) OpenEpub(id int64) (EpubReader, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	f, err := l.store.OpenEpub(b.Location)
	if err != nil {
		log.Printf("open: book %d (%q): %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	return f, nil
}

// ExtractCover returns the cover image bytes from book id's epub.
func (l *libraryImpl) ExtractCover(id int64) ([]byte, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.ExtractCover(b.EpubPath, b.CoverPath)
}

// ExtractOPF returns the raw OPF XML bytes from book id's epub.
func (l *libraryImpl) ExtractOPF(id int64) ([]byte, error) {
	b, err := l.get(id)
	if err != nil {
		return nil, err
	}
	return epub.ExtractOPF(b.EpubPath)
}

// Edit applies edits to the book with the given id, persists everything, and
// returns the updated book. The edit base is the book's current state, fetched
// under the per-book lock — an atomic read-modify-write, so concurrent callers
// cannot revert each other's changes by editing from stale snapshots. If the
// title or authors change, the book directory is moved.
func (l *libraryImpl) Edit(id int64, e model.Edits) (*model.Book, error) {
	mu := l.bookMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return nil, err
	}

	// Every edit is validated here at the facade — the single enforcement
	// point — so meta-only edits (which skip the epub rewrite) can't slip
	// through unchecked.
	e = e.Normalized()
	if v := e.Validate(b); v != nil {
		return nil, v
	}

	updated := applyMeta(b, e)

	op := l.index.BeginOp()

	c, err := epub.Prepare(b, e)
	if err != nil {
		log.Printf("edit: book %d (%q): prepare rewrite: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	if err := op.MarkPending(); err != nil {
		c.Discard()
		return nil, err
	}
	if err := c.Commit(); err != nil {
		c.Discard()
		log.Printf("edit: book %d (%q): commit rewrite: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	if book := c.Book(); book != nil {
		updated.Bib = bibFromEpub(book)
	}

	newLoc := l.store.Layout(updated.Authors, updated.Title, updated.Meta.ID)
	if newLoc.LibraryPath != b.Location.LibraryPath || newLoc.EpubFilename != b.Location.EpubFilename {
		if err := l.store.Move(b.Location, newLoc); err != nil {
			log.Printf("edit: book %d (%q): move directory: %v", b.Meta.ID, b.Title, err)
			return nil, err
		}
		updated.Location = newLoc
	}
	if err := l.store.WriteMeta(updated.Location, &updated.Meta); err != nil {
		log.Printf("edit: book %d (%q): write meta: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}

	mt, err := l.statBook(updated.Location)
	if err != nil {
		log.Printf("edit: book %d (%q): stat: %v", b.Meta.ID, b.Title, err)
		return nil, err
	}
	if err := op.Put(updated, mt); err != nil {
		return nil, err
	}
	return updated, nil
}

// applyMeta returns a copy of b with the meta edits in e applied and the
// modified time stamped. Fields left nil in e are untouched. Bib fields are not
// applied here — Edit derives them from the epub re-parse.
func applyMeta(b *model.Book, e model.Edits) *model.Book {
	cp := *b
	if e.Status != nil {
		cp.Meta.Status = *e.Status
	}
	if e.Rating != nil {
		cp.Meta.Rating = *e.Rating
	}
	if e.Tags != nil {
		cp.Meta.Tags = *e.Tags
	}
	cp.Meta.DateModified = time.Now()
	return &cp
}

// bibFromEpub converts a parsed epub.Book into a model.Bib.
func bibFromEpub(src *epub.Book) model.Bib {
	var series *model.SeriesRef
	if src.Series != "" {
		series = &model.SeriesRef{Name: src.Series, Index: src.SeriesIndex}
	}

	identifiers := make(map[string]string, len(src.Identifiers))
	for _, ident := range src.Identifiers {
		identifiers[ident.ID] = ident.Value
	}

	return model.Bib{
		Title:       src.Title,
		SortTitle:   src.SortTitle,
		Authors:     src.Authors,
		Series:      series,
		Language:    src.Language,
		Pubdate:     src.PubDate,
		Description: src.Description,
		Identifiers: identifiers,
		CoverPath:   src.CoverPath,
		OpfSize:     src.OpfSize,
		CoverSize:   src.CoverSize,
		EpubSize:    src.EpubSize,
	}
}

// Delete removes the book with the given id from the store and the index,
// resolving its current location under the per-book lock.
func (l *libraryImpl) Delete(id int64) error {
	mu := l.bookMu.For(id)
	mu.Lock()
	defer mu.Unlock()

	b, err := l.get(id)
	if err != nil {
		return err
	}
	op := l.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		return err
	}
	// Store is authoritative; a ghost index row is cleaned up by reindex.
	err = l.store.Delete(b.Location)
	if err != nil {
		log.Printf("delete: book %d (%q): %v", id, b.Title, err)
		return err
	}
	if err := op.Delete(id); err != nil {
		log.Printf("delete: book %d (%q): %v", id, b.Title, err)
		return err
	}
	log.Printf("delete: book %d (%q): ok", id, b.Title)
	return nil
}
