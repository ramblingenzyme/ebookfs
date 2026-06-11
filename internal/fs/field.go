package fs

import (
	"errors"
	"strings"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
)

// fieldFile is a readable/writable file backed by a single string-valued field.
// Content is snapshotted per fid on Open; writes are buffered per fid and
// committed (trimmed of trailing newline) when the fid is closed.
type fieldFile struct {
	fs.BaseFile
	get    func() string
	set    func(string) error
	reads  map[uint64][]byte
	writes map[uint64][]byte
}

func newFieldFile(stat *proto.Stat, get func() string, set func(string) error) *fieldFile {
	return &fieldFile{
		BaseFile: *fs.NewBaseFile(stat),
		get:      get,
		set:      set,
		reads:    make(map[uint64][]byte),
		writes:   make(map[uint64][]byte),
	}
}

func (f *fieldFile) Open(fid uint64, omode proto.Mode) error {
	f.Lock()
	defer f.Unlock()
	f.reads[fid] = []byte(f.get() + "\n")
	f.writes[fid] = nil
	return nil
}

func (f *fieldFile) Read(fid uint64, offset uint64, count uint64) ([]byte, error) {
	f.RLock()
	defer f.RUnlock()
	data := f.reads[fid]
	if offset >= uint64(len(data)) {
		return []byte{}, nil
	}
	if offset+count > uint64(len(data)) {
		count = uint64(len(data)) - offset
	}
	return data[offset : offset+count], nil
}

func (f *fieldFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	f.Lock()
	defer f.Unlock()
	end := offset + uint64(len(data))
	buf := f.writes[fid]
	if end > uint64(len(buf)) {
		buf = append(buf, make([]byte, end-uint64(len(buf)))...)
	}
	copy(buf[offset:], data)
	f.writes[fid] = buf
	return uint32(len(data)), nil
}

func (f *fieldFile) Close(fid uint64) error {
	f.Lock()
	defer f.Unlock()
	data := f.writes[fid]
	delete(f.reads, fid)
	delete(f.writes, fid)
	if len(data) == 0 {
		return nil
	}
	if f.set == nil {
		return errors.New("read-only")
	}
	return f.set(strings.TrimRight(string(data), "\n"))
}
