package kosync

import (
	"crypto/md5"
	"encoding/hex"
	"io"
)

// PartialMD5 ports KOReader's partial_md5_checksum, the kosync document ID
// (docs/kosync.md). It hashes up to 12 samples of 1024 bytes at exponentially
// spaced offsets and returns the MD5 as lowercase hex.
//
// The offsets are lshift(1024, 2*i) for i = -1..10. The first is 0, since
// LuaJIT masks the shift to 32 bits and lshift(1024, -2) wraps to 0.
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
