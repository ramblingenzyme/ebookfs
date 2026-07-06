package fs

import (
	"errors"
	"io"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
)

// snapshotFile is a base for files whose content is loaded once on Open via an
// injected load func and served per-fid as a []byte slice. Open loads the data
// and caches it per fid; Read returns clamped sub-slices of the cached bytes;
// Close cleans up the per-fid entry.
type snapshotFile struct {
	fs.BaseFile
	load  func() ([]byte, error)
	reads map[uint64][]byte
}

func newSnapshotFile(stat *proto.Stat, load func() ([]byte, error)) snapshotFile {
	return snapshotFile{
		BaseFile: *fs.NewBaseFile(stat),
		load:     load,
		reads:    make(map[uint64][]byte),
	}
}

func (f *snapshotFile) Open(fid uint64, _ proto.Mode) error {
	data, err := f.load()
	if err != nil {
		return err
	}
	f.Lock()
	f.reads[fid] = data
	f.Unlock()
	return nil
}

func (f *snapshotFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
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

func (f *snapshotFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	delete(f.reads, fid)
	return nil
}

// readAtFile is a base for files that hold one EpubReader per fid, acquired
// via an injected open func. Read delegates to ReadAt and swallows io.EOF
// (the reader may return its final bytes and EOF in a single call).
type readAtFile struct {
	fs.BaseFile
	open func() (library.EpubReader, error)
	fids map[uint64]library.EpubReader
}

func newReadAtFile(stat *proto.Stat, open func() (library.EpubReader, error)) readAtFile {
	return readAtFile{
		BaseFile: *fs.NewBaseFile(stat),
		open:     open,
		fids:     make(map[uint64]library.EpubReader),
	}
}

func (f *readAtFile) Open(fid uint64, _ proto.Mode) error {
	r, err := f.open()
	if err != nil {
		return err
	}
	f.Lock()
	f.fids[fid] = r
	f.Unlock()
	return nil
}

func (f *readAtFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	f.RLock()
	defer f.RUnlock()
	r := f.fids[fid]
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

func (f *readAtFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	if r, ok := f.fids[fid]; ok {
		r.Close()
		delete(f.fids, fid)
	}
	return nil
}
