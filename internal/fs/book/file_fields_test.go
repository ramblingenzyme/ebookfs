package book

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

func testFieldFileStat(t *testing.T, mode uint32) *proto.Stat {
	return newStat(testutil.NewTestFS(t), "test", mode)
}

func TestFieldFileRead(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return "hello" }, nil)

	if got := fstest.Fid(t, ff, 1).Get(proto.Mode(0), 10); got != "hello\n" {
		t.Errorf("Read = %q, want %q", got, "hello\n")
	}
}

// Read clamping, past-end, and unopened-fid behavior come from the embedded
// snapshotFile, whose base test owns them. The reads kept here exercise
// fieldFile's own load wrapping (the trailing "\n").

func TestFieldFileReadEmpty(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return "" }, nil)

	if got := fstest.Fid(t, ff, 1).Get(proto.Mode(0), 10); got != "\n" {
		t.Errorf("Read(empty field) = %q, want %q", got, "\n")
	}
}

func TestFieldFileWriteClose(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "" }, func(s string) error {
		got = s
		return nil
	})

	fstest.Fid(t, ff, 1).Set(proto.Mode(0), "new value")

	if got != "new value" {
		t.Errorf("set was called with %q, want %q", got, "new value")
	}
}

func TestFieldFileWriteTrailingNewlineTrimmed(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "" }, func(s string) error {
		got = s
		return nil
	})

	fstest.Fid(t, ff, 1).Set(proto.Mode(0), "value\n")

	if got != "value" {
		t.Errorf("set was called with %q, want %q", got, "value")
	}
}

func TestFieldFileNoWriteDoesNotCallSet(t *testing.T) {
	called := false
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "" }, func(s string) error {
		called = true
		return nil
	})

	fid := fstest.Fid(t, ff, 1)
	fid.Open(proto.Mode(0))
	fid.Close()

	if called {
		t.Error("set was called even though no data was written")
	}
}

func TestFieldFileWriteReadOnly(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return "val" }, nil)

	fid := fstest.Fid(t, ff, 1)
	fid.Open(proto.Mode(0))
	fid.Write(0, "new")

	if err := fid.WantCloseError(); err.Error() != "read-only" {
		t.Errorf("got error %q, want %q", err.Error(), "read-only")
	}
}

func TestFieldFileStatLength(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return "hello" }, nil)

	s := ff.Stat()
	if s.Length != 6 {
		t.Errorf("Stat().Length = %d, want 6 (hello + \\n)", s.Length)
	}
}

func TestFieldFileStatLengthEmpty(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return "" }, nil)

	s := ff.Stat()
	if s.Length != 1 {
		t.Errorf("Stat().Length for empty field = %d, want 1 (just \\n)", s.Length)
	}
}

func TestFieldFilePerFidBuffers(t *testing.T) {
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "original" }, nil)

	fid1, fid2 := fstest.Fid(t, ff, 1), fstest.Fid(t, ff, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))

	fid1.Write(0, "fid1 write")

	// Reads return the Open snapshot regardless of writes.
	if got := fid1.Read(0, 20); got != "original\n" {
		t.Errorf("fid1 read = %q, want %q", got, "original\n")
	}
	if got := fid2.Read(0, 20); got != "original\n" {
		t.Errorf("fid2 read = %q, want %q", got, "original\n")
	}
}

func TestFieldFileGetUpdatesOnReopen(t *testing.T) {
	value := "first"
	ff := newFieldFile(testFieldFileStat(t, 0444), func() string { return value }, nil)

	fid := fstest.Fid(t, ff, 1)
	if got := fid.Get(proto.Mode(0), 10); got != "first\n" {
		t.Errorf("Read = %q, want %q", got, "first\n")
	}

	fid.Close()
	value = "second"

	if got := fid.Get(proto.Mode(0), 10); got != "second\n" {
		t.Errorf("Read after reopen = %q, want %q", got, "second\n")
	}
}

func TestFieldFileOtruncOverwrite(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "oldvalue" }, func(s string) error {
		got = s
		return nil
	})

	fstest.Fid(t, ff, 1).Set(proto.Otrunc, "new")

	if got != "new" {
		t.Errorf("set was called with %q, want %q", got, "new")
	}
}

func TestFieldFileAppendWithoutOtrunc(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "old" }, func(s string) error {
		got = s
		return nil
	})

	fid := fstest.Fid(t, ff, 1)
	fid.Open(proto.Mode(0))
	// Snapshot is "old\n" = 4 bytes. Write at end.
	fid.Write(4, "new\n")
	fid.Close()

	if got != "old\nnew" {
		t.Errorf("set was called with %q, want %q", got, "old\nnew")
	}
}

func TestFieldFilePartialOverwriteWithoutOtrunc(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "hello world" }, func(s string) error {
		got = s
		return nil
	})

	fid := fstest.Fid(t, ff, 1)
	fid.Open(proto.Mode(0))
	// Replace "lo " at offset 3 with "XY".
	fid.Write(3, "XY")
	fid.Close()

	if got != "helXY world" {
		t.Errorf("set was called with %q, want %q", got, "helXY world")
	}
}

func TestFieldFileShorterOverwriteWithoutOtrunc(t *testing.T) {
	var got string
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "reading" }, func(s string) error {
		got = s
		return nil
	})

	// Write "read\n" at offset 0, shorter than the snapshot "reading\n". Without
	// the fix, residual bytes produce "read\ning".
	fstest.Fid(t, ff, 1).Set(proto.Mode(0), "read\n")

	if got != "read" {
		t.Errorf("set was called with %q, want %q", got, "read")
	}
}

func TestFieldFileOtruncNoWriteDoesNotCallSet(t *testing.T) {
	called := false
	ff := newFieldFile(testFieldFileStat(t, 0644), func() string { return "old" }, func(s string) error {
		called = true
		return nil
	})

	fid := fstest.Fid(t, ff, 1)
	fid.Open(proto.Otrunc)
	fid.Close()

	if called {
		t.Error("set was called even though no data was written")
	}
}
