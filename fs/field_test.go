package fs

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
)

func TestFieldFileRead(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "hello" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ff.Read(fid, 0, 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("Read = %q, want %q", data, "hello\n")
	}
}

func TestFieldFilePartialRead(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "abcdefghij" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ff.Read(fid, 3, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "defgh" {
		t.Errorf("Read(offset=3, count=5) = %q, want %q", data, "defgh")
	}
}

func TestFieldFileReadEmpty(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ff.Read(fid, 0, 10)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "\n" {
		t.Errorf("Read(empty field) = %q, want %q", data, "\n")
	}
}

func TestFieldFileReadPastEnd(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "hi" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ff.Read(fid, 100, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read(past end) = %d bytes, want 0", len(data))
	}
}

func TestFieldFileWriteClose(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := ff.Write(fid, 0, []byte("new value")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "new value" {
		t.Errorf("set was called with %q, want %q", got, "new value")
	}
}

func TestFieldFileWriteTrailingNewlineTrimmed(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := ff.Write(fid, 0, []byte("value\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "value" {
		t.Errorf("set was called with %q, want %q", got, "value")
	}
}

func TestFieldFileNoWriteDoesNotCallSet(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	called := false
	ff := newFieldFile(stat, func() string { return "" }, func(s string) error {
		called = true
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if called {
		t.Error("set was called even though no data was written")
	}
}

func TestFieldFileWriteReadOnly(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "val" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := ff.Write(fid, 0, []byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := ff.Close(fid)
	if err == nil {
		t.Fatal("expected error writing to read-only fieldFile")
	}
	if err.Error() != "read-only" {
		t.Errorf("got error %q, want %q", err.Error(), "read-only")
	}
}

func TestFieldFileStatLength(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "hello" }, nil)

	s := ff.Stat()
	if s.Length != 6 {
		t.Errorf("Stat().Length = %d, want 6 (hello + \\n)", s.Length)
	}
}

func TestFieldFileStatLengthEmpty(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "" }, nil)

	s := ff.Stat()
	if s.Length != 1 {
		t.Errorf("Stat().Length for empty field = %d, want 1 (just \\n)", s.Length)
	}
}

func TestFieldFilePerFidBuffers(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)
	ff := newFieldFile(stat, func() string { return "original" }, nil)

	fid1, fid2 := uint64(1), uint64(2)
	if err := ff.Open(fid1, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid1: %v", err)
	}
	if err := ff.Open(fid2, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid2: %v", err)
	}

	if _, err := ff.Write(fid1, 0, []byte("fid1 write")); err != nil {
		t.Fatalf("Write fid1: %v", err)
	}

	// Reads return the Open snapshot regardless of writes.
	for _, tc := range []struct {
		fid  uint64
		want string
	}{
		{fid1, "original\n"},
		{fid2, "original\n"},
	} {
		data, err := ff.Read(tc.fid, 0, 20)
		if err != nil {
			t.Fatalf("Read fid %d: %v", tc.fid, err)
		}
		if string(data) != tc.want {
			t.Errorf("Read fid %d = %q, want %q", tc.fid, data, tc.want)
		}
	}
}

func TestFieldFileUnopenedFidReturnsError(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)
	ff := newFieldFile(stat, func() string { return "val" }, nil)

	_, err := ff.Read(42, 0, 10)
	if err == nil {
		t.Error("expected error reading from unopened fid")
	}
}

func TestFieldFileGetUpdatesOnReopen(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0444)

	value := "first"
	ff := newFieldFile(stat, func() string { return value }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, _ := ff.Read(fid, 0, 10)
	if string(data) != "first\n" {
		t.Errorf("Read = %q, want %q", data, "first\n")
	}

	ff.Close(fid)
	value = "second"

	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Reopen: %v", err)
	}

	data, _ = ff.Read(fid, 0, 10)
	if string(data) != "second\n" {
		t.Errorf("Read after reopen = %q, want %q", data, "second\n")
	}
}

func TestFieldFileOtruncOverwrite(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "oldvalue" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Otrunc); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := ff.Write(fid, 0, []byte("new")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "new" {
		t.Errorf("set was called with %q, want %q", got, "new")
	}
}

func TestFieldFileAppendWithoutOtrunc(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "old" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Snapshot is "old\n" = 4 bytes. Write at end.
	if _, err := ff.Write(fid, 4, []byte("new\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "old\nnew" {
		t.Errorf("set was called with %q, want %q", got, "old\nnew")
	}
}

func TestFieldFilePartialOverwriteWithoutOtrunc(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "hello world" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Replace "lo " at offset 3 with "XY".
	if _, err := ff.Write(fid, 3, []byte("XY")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "helXY world" {
		t.Errorf("set was called with %q, want %q", got, "helXY world")
	}
}

func TestFieldFileShorterOverwriteWithoutOtrunc(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	var got string
	ff := newFieldFile(stat, func() string { return "reading" }, func(s string) error {
		got = s
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Write "read\n" at offset 0 — shorter than the snapshot "reading\n".
	// Without the fix, residual bytes produce "read\ning".
	if _, err := ff.Write(fid, 0, []byte("read\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got != "read" {
		t.Errorf("set was called with %q, want %q", got, "read")
	}
}

func TestFieldFileOtruncNoWriteDoesNotCallSet(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)

	called := false
	ff := newFieldFile(stat, func() string { return "old" }, func(s string) error {
		called = true
		return nil
	})

	fid := uint64(1)
	if err := ff.Open(fid, proto.Otrunc); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := ff.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if called {
		t.Error("set was called even though no data was written")
	}
}

func TestFieldFileWriteExceedsLimit(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)
	ff := newFieldFile(stat, func() string { return "" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err := ff.Write(fid, maxFieldFileSize, []byte("x"))
	if err == nil {
		t.Fatal("expected error writing past field file size limit")
	}
}

func TestFieldFileWriteAtLimitAllowed(t *testing.T) {
	f := newTestFS(t)
	stat := f.NewStat("test", "glenda", "glenda", 0644)
	ff := newFieldFile(stat, func() string { return "" }, nil)

	fid := uint64(1)
	if err := ff.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	_, err := ff.Write(fid, maxFieldFileSize-4, []byte("test"))
	if err != nil {
		t.Errorf("write at limit should succeed, got: %v", err)
	}
}
