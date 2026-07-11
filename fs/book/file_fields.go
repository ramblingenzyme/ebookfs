package book

import (
	"errors"
	"strings"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
)

const maxFieldFileSize = 1 << 20 // 1 MiB

// fieldFile is a readable/writable file backed by a single string-valued field.
// Content is snapshotted per fid on Open; writes are buffered per fid and
// committed (trimmed of trailing newline) when the fid is closed.
//
// When the client opens with Otrunc (shell >), the write buffer starts empty —
// the first write completely replaces the field value. Without Otrunc (>> or
// in-place edit), the write buffer starts as a copy of the current value so
// the client can append, edit a middle offset, etc. A first write at offset 0
// that is shorter than the current value replaces it entirely (no trailing
// bytes from the old value leak through). On Close the result is sent through
// set → edits → Validate; an error aborts the commit.
type fieldFile struct {
	vfile.SnapshotFile
	get       func() string
	set       func(string) error
	writes    vfile.WriteBuffer
	truncated map[uint64]bool
}

func newFieldFile(stat *proto.Stat, get func() string, set func(string) error) *fieldFile {
	return &fieldFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, func() ([]byte, error) {
			return []byte(get() + "\n"), nil
		}),
		get:       get,
		set:       set,
		writes:    vfile.NewWriteBuffer(maxFieldFileSize),
		truncated: make(map[uint64]bool),
	}
}

func (f *fieldFile) Stat() proto.Stat {
	s := f.BaseFile.Stat()
	// +1 for the trailing newline that Read and Open always append.
	s.Length = uint64(len(f.get()) + 1)
	return s
}

func (f *fieldFile) Open(fid uint64, omode proto.Mode) error {
	// The base loads and caches the per-fid snapshot (and self-locks); we then
	// record whether the client asked for truncation.
	if err := f.SnapshotFile.Open(fid, omode); err != nil {
		return err
	}
	f.Lock()
	f.truncated[fid] = omode&proto.Otrunc != 0
	f.Unlock()
	return nil
}

func (f *fieldFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	// Otrunc (or a missing snapshot) starts the buffer empty; otherwise the
	// first write seeds it from the current value so the client can append or
	// edit at a middle offset. The buffer's replace-on-shorter-first-write rule
	// handles clients like Linux v9fs on 9P2000 that don't send Otrunc.
	var seed func() []byte
	f.RLock()
	truncated := f.truncated[fid]
	f.RUnlock()
	if !truncated {
		if snapshot, ok := f.Snapshot(fid); ok {
			seed = func() []byte { return snapshot }
		}
	}
	return f.writes.Write(fid, offset, data, seed)
}

func (f *fieldFile) Close(fid uint64) error {
	data := f.writes.Take(fid)
	f.Lock()
	delete(f.truncated, fid)
	f.Unlock()
	// Returns error but internally always returns nil...
	f.SnapshotFile.Close(fid)
	if len(data) == 0 {
		return nil
	}
	if f.set == nil {
		return errors.New("read-only")
	}
	return f.set(strings.TrimRight(string(data), "\n"))
}
