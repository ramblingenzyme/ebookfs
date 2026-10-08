package kosync

import (
	"crypto/md5"
	"encoding/hex"
	"io"
)

// PartialMD5 computes the kosync document ID using KOReader's partial MD5
// algorithm. It takes up to 12 samples of 1024 bytes at exponentially spaced
// offsets, concatenates them, and returns the MD5 hash as lowercase hex.
//
// The offsets are computed as: for i = -1..10: lshift(1024, 2*i)
// The first offset is 0 due to LuaJIT's bit masking behavior (lshift(1024, -2)
// wraps to 0 in 32-bit arithmetic).
//
// See specs/kosync-1.0.md §8.2-8.4 for the full algorithm specification.
func PartialMD5(r io.ReaderAt, size int64) (string, error) {
	const sampleSize = 1024
	h := md5.New()

	// Compute offsets: i=-1 produces 0 (LuaJIT masking), then 1024<<(2*i) for i=0..10
	var offsets [12]int64
	offsets[0] = 0 // lshift(1024, -2) wraps to 0 in LuaJIT's 32-bit masking
	for i := 0; i <= 10; i++ {
		offsets[i+1] = 1024 << (2 * i)
	}

	for _, offset := range offsets {
		if offset >= size {
			break
		}

		// Calculate how many bytes to read (up to sampleSize, but not past EOF)
		remaining := size - offset
		toRead := int64(sampleSize)
		if remaining < toRead {
			toRead = remaining
		}

		buf := make([]byte, toRead)
		n, err := r.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return "", err
		}
		if n != int(toRead) {
			return "", io.ErrUnexpectedEOF
		}

		if _, err := h.Write(buf); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
