package views

import (
	"strconv"
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

// One search handle: the subtree a client gets back from clone, and what
// writing a query to it does. view_search.go is the other half, where handles
// are allocated and reclaimed; a handle reaches back there once, to remove
// itself on close.

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
		StaticDir:     fs.NewStaticDir(newDirStat(f, idStr)),
		id:            id,
		reg:           reg,
		search:        search,
		lastQueryTime: time.Now(),
	}

	resultsStat := newDirStat(f, "results")
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
	s := f.writes.TakeText(fid)
	_ = f.SnapshotFile.Close(fid)
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
