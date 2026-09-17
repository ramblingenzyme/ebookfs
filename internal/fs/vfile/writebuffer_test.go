package vfile

import (
	"math"
	"testing"
)

// TestWriteBufferAccumulates covers the shape of one fid's buffered writes: a
// 9P client sends a file's contents as a sequence of offset writes, and the
// buffer has to reassemble them before the embedding file commits on clunk.
func TestWriteBufferAccumulates(t *testing.T) {
	tests := []struct {
		name   string
		writes []struct {
			offset uint64
			data   string
		}
		want string
	}{
		{
			"single write",
			[]struct {
				offset uint64
				data   string
			}{{0, "hello"}},
			"hello",
		},
		{
			"sequential writes append",
			[]struct {
				offset uint64
				data   string
			}{{0, "hello "}, {6, "world"}},
			"hello world",
		},
		{
			"a later write overwrites in place",
			[]struct {
				offset uint64
				data   string
			}{{0, "hello world"}, {6, "WORLD"}},
			"hello WORLD",
		},
		{
			// A gap is zero-filled rather than rejected: 9P offsets are
			// client-chosen and need not be contiguous.
			"a gap is zero-filled",
			[]struct {
				offset uint64
				data   string
			}{{0, "ab"}, {4, "cd"}},
			"ab\x00\x00cd",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriteBuffer(4096)
			for _, wr := range tc.writes {
				n, err := w.Write(1, wr.offset, []byte(wr.data), nil)
				if err != nil {
					t.Fatalf("Write at %d: %v", wr.offset, err)
				}
				if int(n) != len(wr.data) {
					t.Errorf("Write returned %d, want %d — 9P treats a short count as a partial write", n, len(wr.data))
				}
			}
			if got := string(w.Take(1)); got != tc.want {
				t.Errorf("buffer = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestWriteBufferSeed covers the seeded case, used by files that let a client
// edit the current value rather than replace it wholesale.
func TestWriteBufferSeed(t *testing.T) {
	seed := func() []byte { return []byte("original") }

	t.Run("a mid-file write edits the seeded content", func(t *testing.T) {
		w := NewWriteBuffer(4096)
		if _, err := w.Write(1, 4, []byte("XXXX"), seed); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := string(w.Take(1)); got != "origXXXX" {
			t.Errorf("buffer = %q, want %q", got, "origXXXX")
		}
	})

	t.Run("a shorter write at offset 0 replaces it entirely", func(t *testing.T) {
		// Linux v9fs on 9P2000 doesn't send Otrunc, so a full-file rewrite
		// arrives as a plain write at 0. Without the truncation the tail of the
		// old value would survive: "new" over "original" would read "newginal".
		w := NewWriteBuffer(4096)
		if _, err := w.Write(1, 0, []byte("new"), seed); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := string(w.Take(1)); got != "new" {
			t.Errorf("buffer = %q, want %q — residual seed content leaked through", got, "new")
		}
	})

	t.Run("the seed is only consulted on the first write", func(t *testing.T) {
		var calls int
		counting := func() []byte { calls++; return []byte("original") }

		w := NewWriteBuffer(4096)
		for _, off := range []uint64{0, 8, 16} {
			if _, err := w.Write(1, off, []byte("xxxxxxxx"), counting); err != nil {
				t.Fatalf("Write at %d: %v", off, err)
			}
		}
		if calls != 1 {
			t.Errorf("seed called %d times, want 1 — later writes must build on the accumulated buffer", calls)
		}
	})
}

// TestWriteBufferEmptyWrite pins that a zero-length write neither errors nor
// creates a buffer: an empty write must not turn a fid that never wrote into
// one that committed an empty value.
func TestWriteBufferEmptyWrite(t *testing.T) {
	w := NewWriteBuffer(4096)

	n, err := w.Write(1, 0, nil, nil)
	if err != nil || n != 0 {
		t.Errorf("Write(nil) = (%d, %v), want (0, nil)", n, err)
	}
	if got := w.Take(1); got != nil {
		t.Errorf("Take = %q, want nil — an empty write must not create a buffer", got)
	}

	// The consequence that makes the early return load-bearing: recording the
	// fid would mark it as already seeded, so the next real write would build
	// on an empty buffer instead of the file's current value.
	seeded := NewWriteBuffer(4096)
	if _, err := seeded.Write(1, 0, nil, nil); err != nil {
		t.Fatalf("empty Write: %v", err)
	}
	if _, err := seeded.Write(1, 4, []byte("XXXX"), func() []byte { return []byte("original") }); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if got := string(seeded.Take(1)); got != "origXXXX" {
		t.Errorf("buffer = %q, want %q — the empty write consumed the seed", got, "origXXXX")
	}
}

// TestWriteBufferEnforcesLimit covers the size cap. The offset is client
// controlled, so the check has to bound each term separately: offset+len can
// wrap past the limit and admit an allocation the cap exists to prevent.
func TestWriteBufferEnforcesLimit(t *testing.T) {
	const max = 16

	tests := []struct {
		name    string
		offset  uint64
		size    int
		wantErr bool
	}{
		{"exactly at the limit", 0, max, false},
		{"one past the limit", 0, max + 1, true},
		{"offset plus length at the limit", 8, 8, false},
		{"offset plus length past the limit", 8, 9, true},
		{"offset past the limit", max + 1, 1, true},
		// offset + len wraps to a small number; a naive sum check would pass it.
		{"offset that overflows on addition", math.MaxUint64, 8, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriteBuffer(max)

			_, err := w.Write(1, tc.offset, make([]byte, tc.size), nil)

			if tc.wantErr && err == nil {
				t.Errorf("Write(offset=%d, len=%d) succeeded, want it refused by the %d-byte cap", tc.offset, tc.size, max)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Write(offset=%d, len=%d): %v", tc.offset, tc.size, err)
			}
		})
	}
}

// TestWriteBufferTake covers the handoff to the committing file.
func TestWriteBufferTake(t *testing.T) {
	t.Run("a fid that never wrote yields nil", func(t *testing.T) {
		w := NewWriteBuffer(4096)
		if got := w.Take(1); got != nil {
			t.Errorf("Take = %q, want nil so the caller can tell no write happened", got)
		}
	})

	t.Run("taking twice yields nil the second time", func(t *testing.T) {
		w := NewWriteBuffer(4096)
		if _, err := w.Write(1, 0, []byte("data"), nil); err != nil {
			t.Fatalf("Write: %v", err)
		}
		if got := string(w.Take(1)); got != "data" {
			t.Fatalf("first Take = %q, want %q", got, "data")
		}
		if got := w.Take(1); got != nil {
			t.Errorf("second Take = %q, want nil — the buffer must not commit twice", got)
		}
	})
}

// TestWriteBufferPerFidIsolation pins that two clients writing the same file
// concurrently accumulate separately. They share one WriteBuffer, so a buffer
// keyed loosely would let one client's clunk commit the other's bytes.
func TestWriteBufferPerFidIsolation(t *testing.T) {
	w := NewWriteBuffer(4096)

	if _, err := w.Write(1, 0, []byte("first"), nil); err != nil {
		t.Fatalf("Write fid 1: %v", err)
	}
	if _, err := w.Write(2, 0, []byte("second"), nil); err != nil {
		t.Fatalf("Write fid 2: %v", err)
	}

	if got := string(w.Take(1)); got != "first" {
		t.Errorf("fid 1 buffer = %q, want %q", got, "first")
	}
	if got := string(w.Take(2)); got != "second" {
		t.Errorf("fid 2 buffer = %q, want %q — taking one fid disturbed the other", got, "second")
	}
}
