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

	var offsets [12]int64
	for i := 0; i <= 10; i++ {
		offsets[i+1] = 1024 << (2 * i)
	}

	for _, offset := range offsets {
		if offset >= size {
			break
		}
		n := min(sampleSize, size-offset)
		if _, err := io.CopyN(h, io.NewSectionReader(r, offset, n), n); err != nil {
			return "", err
		}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
