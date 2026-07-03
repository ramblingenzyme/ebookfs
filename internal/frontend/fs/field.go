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
//
// When the client opens with Otrunc (shell >), the write buffer starts empty —
// the first write completely replaces the field value. Without Otrunc (>> or
// in-place edit), the write buffer starts as a copy of the current value so
// the client can append, edit a middle offset, etc. On Close the result is
// sent through set → edits → Validate; an error aborts the commit.
type fieldFile struct {
	fs.BaseFile
	get       func() string
	set       func(string) error
	reads     map[uint64][]byte
	writes    map[uint64][]byte
	truncated map[uint64]bool
}

func newFieldFile(stat *proto.Stat, get func() string, set func(string) error) *fieldFile {
	return &fieldFile{
		BaseFile:  *fs.NewBaseFile(stat),
		get:       get,
		set:       set,
		reads:     make(map[uint64][]byte),
		writes:    make(map[uint64][]byte),
		truncated: make(map[uint64]bool),
	}
}

func (f *fieldFile) Stat() proto.Stat {
	s := f.BaseFile.Stat()
	s.Length = uint64(len(f.get() + "\n"))
	return s
}

func (f *fieldFile) Open(fid uint64, omode proto.Mode) error {
	f.Lock()
	defer f.Unlock()
	f.reads[fid] = []byte(f.get() + "\n")
	f.writes[fid] = nil
	f.truncated[fid] = omode&proto.Otrunc != 0
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
	if len(data) == 0 {
		return 0, nil
	}
	f.Lock()
	defer f.Unlock()
	buf := f.writes[fid]
	if buf == nil {
		if f.truncated[fid] {
			buf = []byte{}
		} else if snapshot, ok := f.reads[fid]; ok {
			buf = append([]byte(nil), snapshot...)
		} else {
			buf = []byte{}
		}
	}
	end := offset + uint64(len(data))
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
	delete(f.truncated, fid)
	if len(data) == 0 {
		return nil
	}
	if f.set == nil {
		return errors.New("read-only")
	}
	return f.set(strings.TrimRight(string(data), "\n"))
}
