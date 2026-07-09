package fs

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Read/open/close semantics are covered by the snapshotFile base tests, and the
// write size-limit behavior by TestWriteFileSizeLimits, both in basefile_test.go.
// These tests cover coverFile's own surface: Stat length from CoverSize, and the
// per-fid write buffer committed to Edit on Close.

func newTestCoverFile(t *testing.T, lib fakeLib, edit func(int64, model.Edits) error) *coverFile {
	t.Helper()
	book := makeBook(1, "Test", "Author")
	book.CoverSize = 16
	return newCoverFile(newStat(newTestFS(t), "cover.jpg", 0644), lib, edit, fixed(book))
}

func TestCoverFileStatLength(t *testing.T) {
	cf := newTestCoverFile(t, fakeLib{}, func(int64, model.Edits) error { return nil })

	if s := cf.Stat(); s.Length != 16 {
		t.Errorf("Stat().Length = %d, want 16", s.Length)
	}
}

func TestCoverFileStatLengthNilLib(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(newStat(f, "cover.jpg", 0644), nil, func(int64, model.Edits) error { return nil }, fixed(book))

	if s := cf.Stat(); s.Length != 0 {
		t.Errorf("Stat().Length with nil lib = %d, want 0", s.Length)
	}
}

func TestCoverFileOpenRead(t *testing.T) {
	lib := fakeLib{
		extractCoverFn: func(_ int64) ([]byte, error) { return []byte("cover image data"), nil },
	}
	cf := newTestCoverFile(t, lib, func(int64, model.Edits) error { return nil })

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	data, err := cf.Read(fid, 0, 50)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "cover image data" {
		t.Errorf("Read = %q, want %q", data, "cover image data")
	}
}

func TestCoverFileWriteClose(t *testing.T) {
	var written *[]byte
	lib := fakeLib{
		extractCoverFn: func(_ int64) ([]byte, error) { return []byte("original"), nil },
	}
	cf := newTestCoverFile(t, lib, func(id int64, edits model.Edits) error {
		written = edits.Cover
		return nil
	})

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := cf.Write(fid, 0, []byte("new cover")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := cf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if string(*written) != "new cover" {
		t.Errorf("edit called with %q, want %q", string(*written), "new cover")
	}
}

func TestCoverFileWriteEmptyDoesNotCallEdit(t *testing.T) {
	called := false
	lib := fakeLib{
		extractCoverFn: func(_ int64) ([]byte, error) { return []byte("original"), nil },
	}
	cf := newTestCoverFile(t, lib, func(int64, model.Edits) error {
		called = true
		return nil
	})

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := cf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if called {
		t.Error("edit should not be called when no data was written")
	}
}

func TestCoverFilePerFidBuffers(t *testing.T) {
	lib := fakeLib{
		extractCoverFn: func(_ int64) ([]byte, error) { return []byte("shared"), nil },
	}
	cf := newTestCoverFile(t, lib, func(int64, model.Edits) error { return nil })

	fid1, fid2 := uint64(1), uint64(2)
	cf.Open(fid1, proto.Mode(0))
	cf.Open(fid2, proto.Mode(0))

	cf.Write(fid1, 0, []byte("fid1 data"))

	// Reads return the Open snapshot regardless of writes to any fid.
	data1, _ := cf.Read(fid1, 0, 20)
	data2, _ := cf.Read(fid2, 0, 20)
	if string(data1) != "shared" || string(data2) != "shared" {
		t.Errorf("reads = %q/%q, want both %q", data1, data2, "shared")
	}
}

func TestCoverFileWriteErrorPassesThrough(t *testing.T) {
	lib := fakeLib{
		extractCoverFn: func(_ int64) ([]byte, error) { return []byte("original"), nil },
	}
	cf := newTestCoverFile(t, lib, func(int64, model.Edits) error { return errTest })

	fid := uint64(1)
	cf.Open(fid, proto.Mode(0))
	cf.Write(fid, 0, []byte("data"))

	if err := cf.Close(fid); err != errTest {
		t.Errorf("Close error = %v, want %v", err, errTest)
	}
}
