package vfile

import (
	"errors"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// SnapshotFile is a base for files whose content is loaded once on Open via an
// injected load func and served per-fid.
type SnapshotFile struct {
	fs.BaseFile
	load  func() ([]byte, error)
	reads map[uint64][]byte
}

func NewSnapshotFile(stat *proto.Stat, load func() ([]byte, error)) SnapshotFile {
	return SnapshotFile{
		BaseFile: *fs.NewBaseFile(stat),
		load:     load,
		reads:    make(map[uint64][]byte),
	}
}

func (f *SnapshotFile) Open(fid uint64, _ proto.Mode) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	f.Lock()
	f.reads[fid] = data
	f.Unlock()
	return nil
}

func (f *SnapshotFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	f.RLock()
	defer f.RUnlock()
	data := f.reads[fid]
	if data == nil {
		return nil, errors.New("not open")
	}
	return ClampRead(data, offset, count), nil
}

// ClampRead is the sub-slice a 9P Tread at offset/count yields: empty at or
// past the end, truncated when count overruns.
func ClampRead(data []byte, offset, count uint64) []byte {
	if offset >= uint64(len(data)) {
		return []byte{}
	}
	if offset+count > uint64(len(data)) {
		count = uint64(len(data)) - offset
	}
	return data[offset : offset+count]
}

// Snapshot returns the per-fid bytes cached at Open, for an embedder seeding a
// write buffer from the current value. Self-locking, so a caller must not hold
// the file's lock.
func (f *SnapshotFile) Snapshot(fid uint64) ([]byte, bool) {
	f.RLock()
	defer f.RUnlock()
	data, ok := f.reads[fid]
	return data, ok
}

func (f *SnapshotFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	delete(f.reads, fid)
	return nil
}
