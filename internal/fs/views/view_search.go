package views

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/textfmt"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Each matcher answers for one query field. An empty field imposes no
// constraint and matches everything. Without that guard Query{} matches
// nothing.

// matchesAuthors matches any of q.Authors against either author column, as
// Index.Search does in SQL.
func matchesAuthors(q library.Query, b *library.Book) bool {
	return len(q.Authors) == 0 || slices.ContainsFunc(b.Authors(), func(a library.Author) bool {
		return slices.Contains(q.Authors, a.Name) || slices.Contains(q.Authors, a.SortName)
	})
}

func matchesTags(q library.Query, b *library.Book) bool {
	return len(q.Tags) == 0 || slices.ContainsFunc(q.Tags, func(t string) bool {
		return slices.Contains(b.Tags(), t)
	})
}

func matchesSeries(q library.Query, b *library.Book) bool {
	return len(q.Series) == 0 || (b.HasSeries() && slices.Contains(q.Series, b.SeriesName()))
}

func matchesStatus(q library.Query, b *library.Book) bool {
	return len(q.Status) == 0 || slices.Contains(q.Status, b.Status())
}

func matchesIDs(q library.Query, b *library.Book) bool {
	return len(q.IDs) == 0 || slices.Contains(q.IDs, b.ID())
}

// matchesTitles matches any of q.Titles against the book's title: exactly when
// q.ExactTitles, otherwise as a case-insensitive substring.
func matchesTitles(q library.Query, b *library.Book) bool {
	if len(q.Titles) == 0 {
		return true
	}
	if q.ExactTitles {
		return slices.Contains(q.Titles, b.Title())
	}
	lower := strings.ToLower(b.Title())
	return slices.ContainsFunc(q.Titles, func(title string) bool {
		return strings.Contains(lower, strings.ToLower(title))
	})
}

// makeMatchesFn returns a predicate reporting whether a book matches q. Values
// within a field are OR'd, inside each matcher; fields are AND'd, in the chain
// below.
//
// The predicate is the single membership authority for a search handle.
// ResyncView replays every registered book through it at query time, and
// registry events evaluate it for live updates, so both paths agree by
// construction.
//
// Only the selecting fields are honoured. Query.Order cannot matter: a
// directory is keyed by name, and ordering never changes membership.
//
// Query.Limit would matter, since capping a result set changes which books are
// in it. textfmt.ParseQuery has no syntax that sets it and must not grow one.
// ctl shares that parser, where a limited selection would mutate an arbitrary
// subset of the books the operator named.
func makeMatchesFn(q library.Query) func(*library.Book) bool {
	return func(b *library.Book) bool {
		return matchesAuthors(q, b) && matchesTags(q, b) && matchesSeries(q, b) &&
			matchesStatus(q, b) && matchesIDs(q, b) && matchesTitles(q, b)
	}
}

// searchResultsDir is a live book listing that evaluates membership against a
// query. It implements registry.BookView so books added/edited/deleted through
// the registry are reflected in real time.
type searchResultsDir struct {
	*bookListDir
	mu        sync.RWMutex
	matchesFn func(*library.Book) bool
}

func newSearchResultsDir(stat *proto.Stat) *searchResultsDir {
	return &searchResultsDir{
		bookListDir: newBookListDir(stat),
	}
}

func (d *searchResultsDir) Add(dir *book.BookDir) {
	d.mu.RLock()
	fn := d.matchesFn
	d.mu.RUnlock()
	if fn != nil && fn(dir.Book()) {
		d.bookListDir.Add(dir)
	}
}

// searchCtlFile accepts query writes and the "close" command. Writing a query
// executes the search and populates the results directory. Writing "close"
// tears down the handle.
type searchCtlFile struct {
	vfile.SnapshotFile
	writes vfile.WriteBuffer
	handle *searchHandleDir
}

func newSearchCtlFile(stat *proto.Stat, handle *searchHandleDir) *searchCtlFile {
	return &searchCtlFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, func() ([]byte, error) {
			return []byte(handle.currentQueryText()), nil
		}),
		writes: vfile.NewWriteBuffer(4096),
		handle: handle,
	}
}

func (f *searchCtlFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	return f.writes.Write(fid, offset, data, nil)
}

func (f *searchCtlFile) Close(fid uint64) error {
	buf := f.writes.Take(fid)
	_ = f.SnapshotFile.Close(fid)
	if buf == nil {
		return nil
	}
	s := strings.TrimSpace(string(buf))
	if s == "" {
		return nil
	}
	if s == "close" {
		f.handle.close()
		return nil
	}
	q, err := textfmt.ParseQuery(s)
	if err != nil {
		return err
	}
	f.handle.executeSearch(q, s)
	return nil
}

// searchHandleDir is a per-handle directory containing ctl and results/.
type searchHandleDir struct {
	*fs.StaticDir
	id      int64
	results *searchResultsDir
	reg     *registry.BookRegistry
	search  *searchDir

	// mu guards the query metadata below; executeSearch writes it from ctl
	// clunk goroutines while ctl reads and the cleanup worker read it from
	// others.
	mu            sync.Mutex
	queryText     string
	lastQueryTime time.Time
}

func newSearchHandleDir(f *fs.FS, id int64, reg *registry.BookRegistry, search *searchDir) *searchHandleDir {
	idStr := strconv.FormatInt(id, 10)
	d := &searchHandleDir{
		StaticDir:     fs.NewStaticDir(newStat(f, idStr, 0555|proto.DMDIR)),
		id:            id,
		reg:           reg,
		search:        search,
		lastQueryTime: time.Now(),
	}

	resultsStat := newStat(f, "results", 0555|proto.DMDIR)
	d.results = newSearchResultsDir(resultsStat)
	d.StaticDir.AddChild(d.results)

	ctlStat := newStat(f, "ctl", 0644)
	d.StaticDir.AddChild(newSearchCtlFile(ctlStat, d))

	return d
}

func (h *searchHandleDir) close() {
	h.search.removeHandle(h.id)
}

// currentQueryText returns the last committed query string, for ctl reads.
func (h *searchHandleDir) currentQueryText() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.queryText
}

// lastQuery returns when the handle last executed a query (or was created),
// for the cleanup worker's TTL and eviction ordering.
func (h *searchHandleDir) lastQuery() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastQueryTime
}

// executeSearch swaps the handle's predicate and rebuilds results/ through the
// registry's ResyncView, so the filter swap, the clear, and the repopulation
// are one atomic step serialized against registry Add/Remove/commit, with no
// membership window where events are evaluated against the wrong query, and no
// unlocked mutation of the results listing.
func (h *searchHandleDir) executeSearch(q library.Query, queryText string) {
	h.mu.Lock()
	h.queryText = queryText
	h.lastQueryTime = time.Now()
	h.mu.Unlock()

	fn := makeMatchesFn(q)
	h.reg.ResyncView(h.results, func() {
		h.results.mu.Lock()
		h.results.matchesFn = fn
		h.results.mu.Unlock()
		h.results.bookListDir.clear()
	})
}

// cloneFile is the entry point for allocation. Opening it creates a new search
// handle; reading returns the allocated id. Close is a no-op, since the handle
// persists until "close" is written to its ctl file or the cleanup worker
// reclaims it.
type cloneFile struct {
	fs.BaseFile
	search  *searchDir
	handles map[uint64]int64
}

func (f *cloneFile) Open(fid uint64, _ proto.Mode) error {
	f.Lock()
	if _, exists := f.handles[fid]; !exists {
		f.handles[fid] = 0
	}
	f.Unlock()
	return nil
}

func (f *cloneFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	f.Lock()
	id, ok := f.handles[fid]
	if !ok {
		f.Unlock()
		return nil, fmt.Errorf("not open")
	}
	needAlloc := id == 0
	f.Unlock()

	if needAlloc {
		handle := f.search.allocateHandle()
		f.Lock()
		id = handle.id
		f.handles[fid] = id
		f.Unlock()
	}

	data := []byte(strconv.FormatInt(id, 10) + "\n")
	return vfile.ClampRead(data, offset, count), nil
}

func (f *cloneFile) Close(fid uint64) error {
	f.Lock()
	delete(f.handles, fid)
	f.Unlock()
	return nil
}

type searchDir struct {
	*fs.StaticDir
	f           *fs.FS
	reg         *registry.BookRegistry
	mu          sync.Mutex
	handles     map[int64]*searchHandleDir
	nextID      int64
	ttl         time.Duration
	maxHandles  int
	cleanupDone chan struct{}
	closeOnce   sync.Once
}

func NewSearchDir(f *fs.FS, reg *registry.BookRegistry, ttl time.Duration, maxHandles int) *searchDir {
	d := &searchDir{
		StaticDir:   fs.NewStaticDir(newStat(f, "search", 0555|proto.DMDIR)),
		f:           f,
		reg:         reg,
		handles:     make(map[int64]*searchHandleDir),
		nextID:      1,
		ttl:         ttl,
		maxHandles:  maxHandles,
		cleanupDone: make(chan struct{}),
	}

	clone := &cloneFile{
		BaseFile: *fs.NewBaseFile(newStat(f, "clone", 0444)),
		search:   d,
		handles:  make(map[uint64]int64),
	}
	d.StaticDir.AddChild(clone)

	if ttl > 0 || maxHandles > 0 {
		go d.cleanupLoop()
	}

	return d
}

func (d *searchDir) allocateHandle() *searchHandleDir {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.cleanupLocked(1)

	id := d.nextID
	d.nextID++

	handle := newSearchHandleDir(d.f, id, d.reg, d)
	d.handles[id] = handle
	d.reg.AddView(handle.results)
	d.StaticDir.AddChild(handle)

	return handle
}

func (d *searchDir) removeHandle(id int64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.removeHandleLocked(id)
}

func (d *searchDir) removeHandleLocked(id int64) {
	handle, ok := d.handles[id]
	if !ok {
		return
	}
	d.reg.RemoveView(handle.results)
	d.StaticDir.DeleteChild(strconv.FormatInt(id, 10))
	delete(d.handles, id)
}

// cleanupLocked reclaims handles: first any idle longer than the TTL, then the
// least recently queried until maxHandles-headroom remain. headroom is how many
// handles the caller is about to add. allocateHandle passes 1 so the handle it
// then inserts still fits under the cap, while the periodic sweep, which adds
// nothing, passes 0. Counting only what is already in the map would leave the
// cap admitting maxHandles+1.
func (d *searchDir) cleanupLocked(headroom int) {
	now := time.Now()

	if d.ttl > 0 {
		for id, handle := range d.handles {
			if now.Sub(handle.lastQuery()) > d.ttl {
				d.removeHandleLocked(id)
			}
		}
	}

	if d.maxHandles > 0 && len(d.handles)+headroom > d.maxHandles {
		sorted := slices.Collect(maps.Values(d.handles))
		slices.SortFunc(sorted, func(a, b *searchHandleDir) int {
			return a.lastQuery().Compare(b.lastQuery())
		})
		toRemove := len(sorted) + headroom - d.maxHandles
		for _, h := range sorted[:toRemove] {
			d.removeHandleLocked(h.id)
		}
	}
}

func (d *searchDir) Close() {
	d.closeOnce.Do(func() { close(d.cleanupDone) })
}

// cleanupLoop sweeps expired handles until Close. NewSearchDir starts it with
// nothing synchronising its startup, so a short test can exit before the
// scheduler runs it at all: its five statements record as 0% or 62.5% depending
// on timing, and any coverage gate over this package has to serialize (-p 1).
// The ticker arm never runs under test either way, since five minutes outlives
// every run; only Close is ever observed.
func (d *searchDir) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.mu.Lock()
			d.cleanupLocked(0)
			d.mu.Unlock()
		case <-d.cleanupDone:
			return
		}
	}
}
