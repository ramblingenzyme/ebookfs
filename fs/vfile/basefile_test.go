package vfile

import (
	"bytes"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
)

// These tests cover the read/open/close semantics shared by the two base file
// types. The concrete files (cover/opf, epub, reader, field) embed one of these
// and are tested only for their own surface — construction wiring, Stat, and
// writes — rather than re-checking the base read behavior four times.

// ---- snapshotFile (embedded by coverFile, opfFile, fieldFile) ----

func newTestSnapshotFile(t *testing.T, data []byte) *SnapshotFile {
	t.Helper()
	stat := NewStat(testutil.NewTestFS(t), "snap", 0444)
	sf := NewSnapshotFile(stat, func() ([]byte, error) { return data, nil })
	return &sf
}

func TestSnapshotFileReadClamps(t *testing.T) {
	sf := newTestSnapshotFile(t, []byte("hello world"))
	if err := sf.Open(1, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Partial read returns the requested sub-slice.
	data, err := sf.Read(1, 6, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("Read(6,5) = %q, want %q", data, "world")
	}

	// A count past the end is clamped to what remains.
	data, _ = sf.Read(1, 6, 100)
	if string(data) != "world" {
		t.Errorf("Read(6,100) = %q, want %q", data, "world")
	}

	// An offset past the end returns no bytes rather than erroring.
	data, err = sf.Read(1, 100, 5)
	if err != nil {
		t.Fatalf("Read past end: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read past end = %d bytes, want 0", len(data))
	}
}

func TestSnapshotFileReadUnopenedErrors(t *testing.T) {
	sf := newTestSnapshotFile(t, []byte("data"))
	if _, err := sf.Read(42, 0, 10); err == nil {
		t.Error("expected error reading from an unopened fid")
	}
}

func TestSnapshotFileOpenPropagatesLoadError(t *testing.T) {
	stat := NewStat(testutil.NewTestFS(t), "snap", 0444)
	sf := NewSnapshotFile(stat, func() ([]byte, error) { return nil, testutil.ErrTest })
	if err := sf.Open(1, proto.Mode(0)); err != testutil.ErrTest {
		t.Errorf("Open error = %v, want %v", err, testutil.ErrTest)
	}
}

func TestSnapshotFilePerFidIsolation(t *testing.T) {
	sf := newTestSnapshotFile(t, []byte("shared"))
	sf.Open(1, proto.Mode(0))
	sf.Open(2, proto.Mode(0))

	// Closing one fid leaves the other readable.
	if err := sf.Close(1); err != nil {
		t.Fatalf("Close fid1: %v", err)
	}
	if _, err := sf.Read(1, 0, 6); err == nil {
		t.Error("closed fid should no longer read")
	}
	data, err := sf.Read(2, 0, 6)
	if err != nil {
		t.Fatalf("Read fid2 after fid1 closed: %v", err)
	}
	if string(data) != "shared" {
		t.Errorf("fid2 read = %q, want %q", data, "shared")
	}
}

// ---- readAtFile (embedded by epubFile, readerFile) ----

func newTestReadAtFile(t *testing.T, data string) *ReadAtFile {
	t.Helper()
	stat := NewStat(testutil.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) {
		return &libfake.EpubReader{Reader: bytes.NewReader([]byte(data))}, nil
	})
	return &raf
}

func TestReadAtFileReadClamps(t *testing.T) {
	raf := newTestReadAtFile(t, "hello world")
	if err := raf.Open(1, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := raf.Read(1, 6, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "world" {
		t.Errorf("Read(6,5) = %q, want %q", data, "world")
	}

	// Reading at EOF returns no bytes and swallows io.EOF.
	data, err = raf.Read(1, 100, 5)
	if err != nil {
		t.Fatalf("Read at EOF: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read at EOF = %d bytes, want 0", len(data))
	}
}

func TestReadAtFileReadUnopenedErrors(t *testing.T) {
	raf := newTestReadAtFile(t, "data")
	if _, err := raf.Read(42, 0, 10); err == nil {
		t.Error("expected error reading from an unopened fid")
	}
}

func TestReadAtFileOpenPropagatesError(t *testing.T) {
	stat := NewStat(testutil.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) { return nil, testutil.ErrTest })
	if err := raf.Open(1, proto.Mode(0)); err != testutil.ErrTest {
		t.Errorf("Open error = %v, want %v", err, testutil.ErrTest)
	}
}

func TestReadAtFilePerFidIsolation(t *testing.T) {
	raf := newTestReadAtFile(t, "shared")
	raf.Open(1, proto.Mode(0))
	raf.Open(2, proto.Mode(0))

	if err := raf.Close(1); err != nil {
		t.Fatalf("Close fid1: %v", err)
	}
	data, err := raf.Read(2, 0, 6)
	if err != nil {
		t.Fatalf("Read fid2 after fid1 closed: %v", err)
	}
	if string(data) != "shared" {
		t.Errorf("fid2 read = %q, want %q", data, "shared")
	}
}

func TestReadAtFileCloseReleasesReader(t *testing.T) {
	r := &libfake.EpubReader{Reader: bytes.NewReader([]byte("data"))}
	stat := NewStat(testutil.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) { return r, nil })

	if err := raf.Open(1, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r.Closed {
		t.Fatal("reader should not be closed before Close")
	}
	if err := raf.Close(1); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !r.Closed {
		t.Error("reader should be closed after Close")
	}
}
