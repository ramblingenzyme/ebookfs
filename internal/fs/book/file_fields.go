package book

import (
	"errors"
	"strings"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
)

const maxFieldFileSize = 1 << 20 // 1 MiB

// fieldFile is a readable/writable file backed by a single string-valued field.
// Content is snapshotted per fid on Open; writes are buffered per fid and
// committed (trimmed of trailing newline) when the fid is closed.
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
	// The base loads and caches the per-fid snapshot, and self-locks.
	if err := f.SnapshotFile.Open(fid, omode); err != nil {
		return err
	}
	f.Lock()
	f.truncated[fid] = omode&proto.Otrunc != 0
	f.Unlock()
	return nil
}

func (f *fieldFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	var seed func() []byte
	f.RLock()
	truncated := f.truncated[fid]
	f.RUnlock()
	if !truncated {
		if snapshot, ok := f.Snapshot(fid); ok {
			seed = func() []byte { return snapshot }
		}
	}

	// Linux v9fs on 9P2000 never sends Otrunc, so seed is set above even when
	// the client means to replace. WriteBuffer covers that: a first write at
	// offset 0 shorter than the seed truncates rather than merging.
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
