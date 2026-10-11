package vfile

import (
	"fmt"
	"strings"
	"sync"
)

// WriteBuffer accumulates per-fid 9P write data for files whose writes are
// buffered and committed when the fid is closed (field, cover, and control
// files). It is self-locking with its own mutex, independent of the embedding
// file's, so callers never juggle lock ordering against the base-file methods.
//
// ponytail: a failed commit returns its error in Rclunk, and Linux v9fs clunks
// from ->release, whose result the VFS discards. close(2) returns 0, so
// `echo 7 > rating` or a PNG written over a JPEG cover exits 0 and changes
// nothing, and only the server log says why. inbox/ commits on clunk the same
// way. Surface failures in a per-book errors file or in log/ once a Linux
// mount is the main client.
type WriteBuffer struct {
	mu   sync.Mutex
	max  uint64
	bufs map[uint64][]byte
}

func NewWriteBuffer(max uint64) WriteBuffer {
	return WriteBuffer{max: max, bufs: make(map[uint64][]byte)}
}

// Write applies one 9P write at offset. A non-nil seed gives fid's buffer its
// initial content, so a client can append or edit at a middle offset.
//
// A first write at offset 0 shorter than the seed truncates rather than
// merging, so no residual old content leaks through.
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

// take removes and returns fid's accumulated buffer; nil if fid never wrote.
func (w *WriteBuffer) take(fid uint64) []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	buf := w.bufs[fid]
	delete(w.bufs, fid)
	return buf
}

// Commit hands the buffer to commit and returns its error, unless the fid
// never wrote. The buffer is removed whether or not commit is called.
func (w *WriteBuffer) Commit(fid uint64, commit func([]byte) error) error {
	buf := w.take(fid)
	if len(buf) == 0 {
		return nil
	}
	return commit(buf)
}

// CommitText is Commit for a command line: the bytes are trimmed of
// surrounding whitespace, and nothing is committed if that leaves "".
func (w *WriteBuffer) CommitText(fid uint64, commit func(string) error) error {
	return w.Commit(fid, func(buf []byte) error {
		s := strings.TrimSpace(string(buf))
		if s == "" {
			return nil
		}
		return commit(s)
	})
}
