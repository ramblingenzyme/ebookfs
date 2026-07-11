// Package vfile holds the generic 9P file primitives: the snapshot and read-at
// base types that concrete files embed, plus the NewStat owner convention. It
// depends only on the library interfaces and plain-function callbacks — never on
// the book directory tree, registry, or views — so it is the leaf of the
// frontend.
package vfile

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
)

// SnapshotFile is a base for files whose content is loaded once on Open via an
// injected load func and served per-fid as a []byte slice. Open loads the data
// and caches it per fid; Read returns clamped sub-slices of the cached bytes;
// Close cleans up the per-fid entry. Embedders that override Open/Close manage
// the per-fid snapshot through the exported methods (Snapshot, and Open/Close
// themselves) rather than the private map.
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
	if offset >= uint64(len(data)) {
		return []byte{}, nil
	}
	if offset+count > uint64(len(data)) {
		count = uint64(len(data)) - offset
	}
	return data[offset : offset+count], nil
}

// Snapshot returns the per-fid bytes cached at Open, for embedders (e.g. a
// writable field file) that seed a write buffer from the current value. It is
// self-locking; callers must not hold the file's lock.
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

// ReadAtFile is a base for files that hold one EpubReader per fid, acquired
// via an injected open func. Read delegates to ReadAt and swallows io.EOF
// (the reader may return its final bytes and EOF in a single call).
type ReadAtFile struct {
	fs.BaseFile
	open func() (library.EpubReader, error)
	fids map[uint64]library.EpubReader
}

func NewReadAtFile(stat *proto.Stat, open func() (library.EpubReader, error)) ReadAtFile {
	return ReadAtFile{
		BaseFile: *fs.NewBaseFile(stat),
		open:     open,
		fids:     make(map[uint64]library.EpubReader),
	}
}

func (f *ReadAtFile) Open(fid uint64, _ proto.Mode) error {
	r, err := f.open()
	if err != nil {
		return err
	}
	f.Lock()
	f.fids[fid] = r
	f.Unlock()
	return nil
}

func (f *ReadAtFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	// Copy the reader out and release the lock before the disk read: holding
	// even the read lock across ReadAt would let one slow read plus a queued
	// Open (write lock) stall every other in-flight read of this file. The
	// reader is per-fid and a client never reads a fid it is clunking, so the
	// unlocked ReadAt cannot race its own Close.
	f.RLock()
	r := f.fids[fid]
	f.RUnlock()
	if r == nil {
		return nil, errors.New("not open")
	}
	buf := make([]byte, count)
	n, err := r.ReadAt(buf, int64(offset))
	if err == io.EOF {
		err = nil
	}
	return buf[:n], err
}

func (f *ReadAtFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	if r, ok := f.fids[fid]; ok {
		r.Close()
		delete(f.fids, fid)
	}
	return nil
}
