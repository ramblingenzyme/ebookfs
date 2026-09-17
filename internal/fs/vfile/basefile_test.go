package vfile

import (
	"bytes"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
)

// The read/open/close semantics shared by the two base file types. The concrete
// files (cover/opf, epub, reader, field) embed one of these and are tested only
// for their own surface (construction wiring, Stat, and writes) rather than
// re-checking the base read behavior four times.

// ---- snapshotFile (embedded by coverFile, opfFile, fieldFile) ----

func newTestSnapshotFile(t *testing.T, data []byte) *SnapshotFile {
	t.Helper()
	stat := NewStat(testutil.NewTestFS(t), "snap", 0444)
	sf := NewSnapshotFile(stat, func() ([]byte, error) { return data, nil })
	return &sf
}

func TestSnapshotFileReadClamps(t *testing.T) {
	fid := fstest.Fid(t, newTestSnapshotFile(t, []byte("hello world")), 1)
	fid.Open(proto.Mode(0))

	// Partial read returns the requested sub-slice.
	if got := fid.Read(6, 5); got != "world" {
		t.Errorf("Read(6,5) = %q, want %q", got, "world")
	}

	// A count past the end is clamped to what remains.
	if got := fid.Read(6, 100); got != "world" {
		t.Errorf("Read(6,100) = %q, want %q", got, "world")
	}

	// An offset past the end returns no bytes rather than erroring.
	if got := fid.Read(100, 5); got != "" {
		t.Errorf("Read past end = %q, want no bytes", got)
	}
}

func TestSnapshotFileReadUnopenedErrors(t *testing.T) {
	fstest.Fid(t, newTestSnapshotFile(t, []byte("data")), 42).WantReadError()
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
	fid1, fid2 := fstest.Fid(t, sf, 1), fstest.Fid(t, sf, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))

	// Closing one fid leaves the other readable.
	fid1.Close()
	fid1.WantReadError()
	if got := fid2.Read(0, 6); got != "shared" {
		t.Errorf("fid2 read = %q, want %q", got, "shared")
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
	fid := fstest.Fid(t, newTestReadAtFile(t, "hello world"), 1)
	fid.Open(proto.Mode(0))

	if got := fid.Read(6, 5); got != "world" {
		t.Errorf("Read(6,5) = %q, want %q", got, "world")
	}

	// Reading at EOF returns no bytes and swallows io.EOF.
	if got := fid.Read(100, 5); got != "" {
		t.Errorf("Read at EOF = %q, want no bytes", got)
	}
}

func TestReadAtFileReadUnopenedErrors(t *testing.T) {
	fstest.Fid(t, newTestReadAtFile(t, "data"), 42).WantReadError()
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
	fid1, fid2 := fstest.Fid(t, raf, 1), fstest.Fid(t, raf, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))

	fid1.Close()
	if got := fid2.Read(0, 6); got != "shared" {
		t.Errorf("fid2 read = %q, want %q", got, "shared")
	}
}

func TestReadAtFileCloseReleasesReader(t *testing.T) {
	r := &libfake.EpubReader{Reader: bytes.NewReader([]byte("data"))}
	stat := NewStat(testutil.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) { return r, nil })

	fid := fstest.Fid(t, &raf, 1)
	fid.Open(proto.Mode(0))
	if r.Closed {
		t.Fatal("reader should not be closed before Close")
	}
	fid.Close()
	if !r.Closed {
		t.Error("reader should be closed after Close")
	}
}

// ---- Snapshot ----

// The accessor embedders use to seed a write buffer from the value the client
// opened, so an edit builds on what was read rather than on whatever the file
// says by the time the write lands.
func TestSnapshotFileSnapshot(t *testing.T) {
	sf := newTestSnapshotFile(t, []byte("current value"))

	if _, ok := sf.Snapshot(1); ok {
		t.Error("Snapshot reported data for an unopened fid")
	}

	fid := fstest.Fid(t, sf, 1)
	fid.Open(proto.Mode(0))
	data, ok := sf.Snapshot(1)
	if !ok {
		t.Fatal("Snapshot reported no data for an open fid")
	}
	if !bytes.Equal(data, []byte("current value")) {
		t.Errorf("Snapshot = %q, want %q", data, "current value")
	}

	// Other fids are unaffected, and a clunked fid releases its snapshot.
	if _, ok := sf.Snapshot(2); ok {
		t.Error("Snapshot reported data for a different, unopened fid")
	}
	fid.Close()
	if _, ok := sf.Snapshot(1); ok {
		t.Error("Snapshot still reported data after the fid was clunked")
	}
}
