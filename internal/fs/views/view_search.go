package views

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
)

// searchDir is the search/ root: a clone file and one subdirectory per live
// handle. A handle costs a goroutine's worth of nothing but holds every
// matching book, so the two caps bound what an abandoned one can retain, and a
// cleanup worker applies them. Zero for either cap turns that worker off.
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
		StaticDir:   fs.NewStaticDir(newDirStat(f, "search")),
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
