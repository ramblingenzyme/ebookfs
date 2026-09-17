package vfile

import (
	"fmt"
	"sync"
)

// WriteBuffer accumulates per-fid 9P write data for files whose writes are
// buffered and committed when the fid is closed (cover and field files). It is
// self-locking with its own mutex, independent of the embedding file's, so
// callers never juggle lock ordering against the base-file methods.
type WriteBuffer struct {
	mu   sync.Mutex
	max  uint64
	bufs map[uint64][]byte
}

func NewWriteBuffer(max uint64) WriteBuffer {
	return WriteBuffer{max: max, bufs: make(map[uint64][]byte)}
}

// Write applies one 9P write at offset, growing fid's buffer as needed. seed,
// when non-nil, provides the buffer's initial content the first time fid
// writes (so a client can append or edit at a middle offset); nil starts
// empty. A first write at offset 0 shorter than the seeded content replaces it
// entirely — the buffer is truncated to the written bytes so residual old
// content can't leak through (Linux v9fs on 9P2000 doesn't send Otrunc).
func (w *WriteBuffer) Write(fid uint64, offset uint64, data []byte, seed func() []byte) (uint32, error) {
	if len(data) == 0 {
		return 0, nil
	}
	// Overflow-safe cap: offset is a client-controlled uint64, so offset+len can
	// wrap past the check. Bound each term against the cap instead of the sum.
	if offset > w.max || uint64(len(data)) > w.max-offset {
		return 0, fmt.Errorf("write exceeds file size limit (%d bytes)", w.max)
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	buf, exists := w.bufs[fid]
	if !exists && seed != nil {
		buf = append([]byte(nil), seed()...)
	}
	end := offset + uint64(len(data))
	if end > uint64(len(buf)) {
		buf = append(buf, make([]byte, end-uint64(len(buf)))...)
	}
	copy(buf[offset:], data)
	if !exists && offset == 0 && end < uint64(len(buf)) {
		buf = buf[:end]
	}
	w.bufs[fid] = buf
	return uint32(len(data)), nil
}

// Take removes and returns fid's accumulated buffer; nil if fid never wrote.
func (w *WriteBuffer) Take(fid uint64) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	buf := w.bufs[fid]
	delete(w.bufs, fid)
	return buf
}
