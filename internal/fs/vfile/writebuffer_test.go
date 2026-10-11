package vfile

import (
	"errors"
	"math"
	"slices"
	"testing"
)

func mustWrite(t *testing.T, w *WriteBuffer, fid uint64, offset uint64, data string, seed func() []byte) {
	t.Helper()
	if _, err := w.Write(fid, offset, []byte(data), seed); err != nil {
		t.Fatalf("Write fid %d at %d: %v", fid, offset, err)
	}
}

// The shape of one fid's buffered writes: a 9P client sends a file's contents
// as a sequence of offset writes, and the buffer has to reassemble them before
// the embedding file commits on clunk.
func TestWriteBufferAccumulates(t *testing.T) {
	type write struct {
		offset uint64
		data   string
	}
	tests := []struct {
		name   string
		writes []write
		want   string
	}{
		{"single write", []write{{0, "hello"}}, "hello"},
		{"sequential writes append", []write{{0, "hello "}, {6, "world"}}, "hello world"},
		{"a later write overwrites in place", []write{{0, "hello world"}, {6, "WORLD"}}, "hello WORLD"},
		// A gap is zero-filled rather than rejected: 9P offsets are
		// client-chosen and need not be contiguous.
		{"a gap is zero-filled", []write{{0, "ab"}, {4, "cd"}}, "ab\x00\x00cd"},
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
			if got := string(w.take(1)); got != tc.want {
				t.Errorf("buffer = %q, want %q", got, tc.want)
			}
		})
	}
}

// The seeded case, used by files that let a client edit the current value
// rather than replace it wholesale.
func TestWriteBufferSeed(t *testing.T) {
	seed := func() []byte { return []byte("original") }

	t.Run("a mid-file write edits the seeded content", func(t *testing.T) {
		w := NewWriteBuffer(4096)
		mustWrite(t, &w, 1, 4, "XXXX", seed)
		if got := string(w.take(1)); got != "origXXXX" {
			t.Errorf("buffer = %q, want %q", got, "origXXXX")
		}
	})

	t.Run("a shorter write at offset 0 replaces it entirely", func(t *testing.T) {
		// Linux v9fs on 9P2000 doesn't send Otrunc, so a full-file rewrite
		// arrives as a plain write at 0. Without the truncation the tail of the
		// old value would survive: "new" over "original" would read "newginal".
		w := NewWriteBuffer(4096)
		mustWrite(t, &w, 1, 0, "new", seed)
		if got := string(w.take(1)); got != "new" {
			t.Errorf("buffer = %q, want %q — residual seed content leaked through", got, "new")
		}
	})

	t.Run("the seed is only consulted on the first write", func(t *testing.T) {
		var calls int
		counting := func() []byte { calls++; return []byte("original") }

		w := NewWriteBuffer(4096)
		for _, off := range []uint64{0, 8, 16} {
			mustWrite(t, &w, 1, off, "xxxxxxxx", counting)
		}
		if calls != 1 {
			t.Errorf("seed called %d times, want 1 — later writes must build on the accumulated buffer", calls)
		}
	})
}

// A zero-length write neither errors nor creates a buffer: an empty write must
// not turn a fid that never wrote into one that committed an empty value.
func TestWriteBufferEmptyWrite(t *testing.T) {
	w := NewWriteBuffer(4096)

	n, err := w.Write(1, 0, nil, nil)
	if err != nil || n != 0 {
		t.Errorf("Write(nil) = (%d, %v), want (0, nil)", n, err)
	}
	if got := w.take(1); got != nil {
		t.Errorf("take = %q, want nil — an empty write must not create a buffer", got)
	}

	// The consequence that makes the early return load-bearing: recording the
	// fid would mark it as already seeded, so the next real write would build
	// on an empty buffer instead of the file's current value.
	seeded := NewWriteBuffer(4096)
	mustWrite(t, &seeded, 1, 0, "", nil)
	mustWrite(t, &seeded, 1, 4, "XXXX", func() []byte { return []byte("original") })
	if got := string(seeded.take(1)); got != "origXXXX" {
		t.Errorf("buffer = %q, want %q — the empty write consumed the seed", got, "origXXXX")
	}
}

// The size cap. The offset is client controlled, so the check has to bound each
// term separately: offset+len can wrap past the limit and admit an allocation
// the cap exists to prevent.
func TestWriteBufferEnforcesLimit(t *testing.T) {
	const maxSize = 16

	tests := []struct {
		name    string
		offset  uint64
		size    int
		wantErr bool
	}{
		{"exactly at the limit", 0, maxSize, false},
		{"one past the limit", 0, maxSize + 1, true},
		{"offset plus length at the limit", 8, 8, false},
		{"offset plus length past the limit", 8, 9, true},
		{"offset past the limit", maxSize + 1, 1, true},
		// offset + len wraps to a small number; a naive sum check would pass it.
		{"offset that overflows on addition", math.MaxUint64, 8, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriteBuffer(maxSize)

			_, err := w.Write(1, tc.offset, make([]byte, tc.size), nil)

			if tc.wantErr && err == nil {
				t.Errorf("Write(offset=%d, len=%d) succeeded, want it refused by the %d-byte cap", tc.offset, tc.size, maxSize)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("Write(offset=%d, len=%d): %v", tc.offset, tc.size, err)
			}
		})
	}
}

// Commit hands the buffer to the commit callback, but only if the fid wrote,
// and only once.
func TestWriteBufferCommit(t *testing.T) {
	for _, tc := range []struct {
		name    string
		write   string
		commits int
		want    []string
	}{
		{"a fid that never wrote does not call commit", "", 1, nil},
		{"a fid that wrote calls commit with its data", "data", 1, []string{"data"}},
		{"a second Commit does not call commit", "data", 2, []string{"data"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriteBuffer(4096)
			if tc.write != "" {
				mustWrite(t, &w, 1, 0, tc.write, nil)
			}
			var got []string
			for range tc.commits {
				if err := w.Commit(1, func(data []byte) error {
					got = append(got, string(data))
					return nil
				}); err != nil {
					t.Fatalf("Commit: %v", err)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("commit called with %q, want %q", got, tc.want)
			}
		})
	}

	t.Run("commit returns the callback's error", func(t *testing.T) {
		w := NewWriteBuffer(4096)
		mustWrite(t, &w, 1, 0, "data", nil)
		wantErr := errors.New("commit failed")
		if err := w.Commit(1, func([]byte) error { return wantErr }); err != wantErr {
			t.Errorf("Commit returned %v, want %v", err, wantErr)
		}
	})
}

// CommitText is for command-line files: it trims whitespace and skips commit if empty.
func TestWriteBufferCommitText(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write string
		want  []string
	}{
		{"a fid that never wrote does not call commit", "", nil},
		{"a fid that wrote only whitespace does not call commit", "  \n", nil},
		{"a fid that wrote a command calls commit with the trimmed text", "  cmd \n", []string{"cmd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := NewWriteBuffer(4096)
			if tc.write != "" {
				mustWrite(t, &w, 1, 0, tc.write, nil)
			}
			var got []string
			if err := w.CommitText(1, func(s string) error {
				got = append(got, s)
				return nil
			}); err != nil {
				t.Errorf("CommitText: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("commit called with %q, want %q", got, tc.want)
			}
		})
	}
}

// Two clients writing the same file concurrently accumulate separately. They
// share one WriteBuffer, so a buffer keyed loosely would let one client's clunk
// commit the other's bytes.
func TestWriteBufferPerFidIsolation(t *testing.T) {
	w := NewWriteBuffer(4096)

	mustWrite(t, &w, 1, 0, "first", nil)
	mustWrite(t, &w, 2, 0, "second", nil)

	if got := string(w.take(1)); got != "first" {
		t.Errorf("fid 1 buffer = %q, want %q", got, "first")
	}
	if got := string(w.take(2)); got != "second" {
		t.Errorf("fid 2 buffer = %q, want %q — taking one fid disturbed the other", got, "second")
	}
}
