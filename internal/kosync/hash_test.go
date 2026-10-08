package kosync

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"
)

// patternFile returns the synthetic test file P(n) from spec §8.6.
// Byte at index i is (i * 31 + (i >> 8)) mod 256.
func patternFile(n int) []byte {
	b := make([]byte, n)
	for i := 0; i < n; i++ {
		b[i] = byte((i*31 + (i >> 8)) % 256)
	}
	return b
}

func TestPartialMD5_GoldenVectors(t *testing.T) {
	// Golden vectors from spec §8.6
	tests := []struct {
		name   string
		size   int
		expect string
	}{
		{"P(500)", 500, "21f0df72cce9bc7da8dae2512ee5feed"},
		{"P(1024)", 1024, "f4cd1641040a17288bb6104d9e66bdb5"},
		{"P(1025)", 1025, "170a888db01c00d8fc3e5d8d84de5838"},
		{"P(2048)", 2048, "d0e59a0c7b893c3b8d6a9bddbd64e631"},
		{"P(3000)", 3000, "d0e59a0c7b893c3b8d6a9bddbd64e631"}, // same as P(2048) per spec §8.5
		{"P(200000)", 200000, "1784b150454ef4cf6780ceb94af0386f"},
		{"P(1050000)", 1050000, "0fca5c751677ecbc2dcfe80ea75ace32"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := patternFile(tt.size)
			r := bytes.NewReader(data)
			got, err := PartialMD5(r, int64(tt.size))
			if err != nil {
				t.Fatalf("PartialMD5: %v", err)
			}
			if got != tt.expect {
				t.Errorf("got %s, want %s", got, tt.expect)
			}
		})
	}
}

// TestPartialMD5_Sparse1GiB exercises all 12 samples using the sparse file S
// from spec §8.6.
func TestPartialMD5_Sparse1GiB(t *testing.T) {
	// S is 1073742848 bytes (1 GiB + 1024), zero everywhere except at each
	// offset where it contains the 16-byte ASCII decimal representation.
	const size = 1073742848
	offsets := []int64{
		0, 1024, 4096, 16384, 65536, 262144,
		1048576, 4194304, 16777216, 67108864, 268435456, 1073741824,
	}

	data := make([]byte, size)
	for _, off := range offsets {
		s := fmt.Sprintf("%016d", off)
		copy(data[off:off+16], s)
	}

	r := bytes.NewReader(data)
	got, err := PartialMD5(r, size)
	if err != nil {
		t.Fatalf("PartialMD5: %v", err)
	}
	expect := "266ff27c24919f4cec96f90f9b3231ca"
	if got != expect {
		t.Errorf("got %s, want %s", got, expect)
	}
}

// TestPartialMD5_FirstOffsetIsZero verifies the i=-1 term produces offset 0,
// per spec §8.3. This is the critical LuaJIT bitwise masking behavior.
func TestPartialMD5_FirstOffsetIsZero(t *testing.T) {
	// Create a file where byte 0 differs from byte 256.
	// If the first sample offset is 0, the hash will differ from a file
	// where only byte 256 differs.
	data1 := patternFile(2048)
	data2 := patternFile(2048)
	data2[0] = 0xFF // change byte at offset 0

	r1 := bytes.NewReader(data1)
	h1, err := PartialMD5(r1, 2048)
	if err != nil {
		t.Fatalf("PartialMD5(data1): %v", err)
	}

	r2 := bytes.NewReader(data2)
	h2, err := PartialMD5(r2, 2048)
	if err != nil {
		t.Fatalf("PartialMD5(data2): %v", err)
	}

	if h1 == h2 {
		t.Error("hashes should differ when byte 0 differs (proves first offset is 0)")
	}

	// Also verify that changing byte 256 produces a different hash.
	data3 := patternFile(2048)
	data3[256] = 0xFF
	r3 := bytes.NewReader(data3)
	h3, err := PartialMD5(r3, 2048)
	if err != nil {
		t.Fatalf("PartialMD5(data3): %v", err)
	}

	if h1 == h3 {
		t.Error("hashes should differ when byte 256 differs")
	}
}

// TestPartialMD5_EmptyFile tests that an empty file produces a valid hash
// (MD5 of empty input).
func TestPartialMD5_EmptyFile(t *testing.T) {
	r := bytes.NewReader(nil)
	got, err := PartialMD5(r, 0)
	if err != nil {
		t.Fatalf("PartialMD5: %v", err)
	}
	// MD5("") = d41d8cd98f00b204e9800998ecf8427e
	expect := "d41d8cd98f00b204e9800998ecf8427e"
	if got != expect {
		t.Errorf("got %s, want %s", got, expect)
	}
}

// TestPartialMD5_FullFileHash verifies that for files larger than 1 GiB,
// the algorithm produces a hash that depends on all 12 samples.
func TestPartialMD5_LowercaseHex(t *testing.T) {
	data := patternFile(2048)
	r := bytes.NewReader(data)
	got, err := PartialMD5(r, 2048)
	if err != nil {
		t.Fatalf("PartialMD5: %v", err)
	}

	// Verify the hash is lowercase hex.
	if len(got) != 32 {
		t.Errorf("hash length = %d, want 32", len(got))
	}
	if strings.ToLower(got) != got {
		t.Errorf("hash %q contains uppercase characters", got)
	}
	// Verify it's valid hex.
	if _, err := hex.DecodeString(got); err != nil {
		t.Errorf("hash %q is not valid hex: %v", got, err)
	}
}
