package book

import (
	"errors"
	"fmt"
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
	writes    map[uint64][]byte
	truncated map[uint64]bool
}

func newFieldFile(stat *proto.Stat, get func() string, set func(string) error) *fieldFile {
	return &fieldFile{
		SnapshotFile: vfile.NewSnapshotFile(stat, func() ([]byte, error) {
			return []byte(get() + "\n"), nil
		}),
		get:       get,
		set:       set,
		writes:    make(map[uint64][]byte),
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
	// initialize our own write-tracking state.
	if err := f.SnapshotFile.Open(fid, omode); err != nil {
		return err
	}
	f.Lock()
	f.writes[fid] = nil
	f.truncated[fid] = omode&proto.Otrunc != 0
	f.Unlock()
	return nil
}

func (f *fieldFile) Write(fid uint64, offset uint64, data []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, nil
	}
	// Overflow-safe cap: offset is a client-controlled uint64, so offset+len can
	// wrap past the check. Bound each term against the cap instead of the sum.
	if offset > maxFieldFileSize || uint64(len(data)) > maxFieldFileSize-offset {
		return 0, fmt.Errorf("write exceeds field file size limit (%d bytes)", maxFieldFileSize)
	}
	// Fetch the snapshot via the base before taking our own lock (the base
	// method self-locks the shared mutex).
	snapshot, hasSnapshot := f.Snapshot(fid)
	f.Lock()
	defer f.Unlock()
	buf := f.writes[fid]
	if buf == nil {
		// Otrunc (or a missing snapshot) starts empty; otherwise seed from the
		// current value so the client can append or edit at a middle offset.
		if f.truncated[fid] || !hasSnapshot {
			buf = []byte{}
		} else {
			buf = append([]byte(nil), snapshot...)
		}
	}
	end := offset + uint64(len(data))
	if end > uint64(len(buf)) {
		buf = append(buf, make([]byte, end-uint64(len(buf)))...)
	}
	copy(buf[offset:], data)

	// Without Otrunc the write buffer is seeded from the old value (snapshot).
	// When writing shorter content at offset 0, truncate the buffer so residual
	// bytes from the old value don't leak through. This matters for clients
	// like Linux v9fs on 9P2000 that don't send Otrunc on file open.
	firstWrite := f.writes[fid] == nil
	shorter := uint64(len(data)) < uint64(len(buf))
	if offset == 0 && firstWrite && !f.truncated[fid] && shorter {
		buf = buf[:len(data)]
	}

	f.writes[fid] = buf
	return uint32(len(data)), nil
}

func (f *fieldFile) Close(fid uint64) error {
	f.Lock()
	data := f.writes[fid]
	delete(f.writes, fid)
	delete(f.truncated, fid)
	f.Unlock()
	// Forget the per-fid snapshot via the base, after releasing our own lock.
	f.SnapshotFile.Close(fid)
	if len(data) == 0 {
		return nil
	}
	if f.set == nil {
		return errors.New("read-only")
	}
	return f.set(strings.TrimRight(string(data), "\n"))
}
