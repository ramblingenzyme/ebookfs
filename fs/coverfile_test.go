package fs

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestCoverFileStatLength(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("cover image data"), nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	s := cf.Stat()
	if s.Length != 16 {
		t.Errorf("Stat().Length = %d, want 16", s.Length)
	}
}

func TestCoverFileStatLengthNilLib(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), nil, book)

	s := cf.Stat()
	if s.Length != 0 {
		t.Errorf("Stat().Length with nil lib = %d, want 0", s.Length)
	}
}

func TestCoverFileOpenRead(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("cover image data"), nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

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

func TestCoverFileReadPartial(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("cover image data"), nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := cf.Read(fid, 6, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "image" {
		t.Errorf("Read(6,5) = %q, want %q", data, "image")
	}
}

func TestCoverFileReadAtEOF(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("hi"), nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := cf.Read(fid, 10, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read at EOF = %d bytes, want 0", len(data))
	}
}

func TestCoverFileOpenError(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return nil, errTest
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	err := cf.Open(1, proto.Mode(0))
	if err != errTest {
		t.Errorf("Open error = %v, want %v", err, errTest)
	}
}

func TestCoverFileWriteClose(t *testing.T) {
	var written []byte
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("original"), nil
		},
		writeCoverFn: func(b *model.Book, data []byte) error {
			written = data
			return nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

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

	if string(written) != "new cover" {
		t.Errorf("WriteCover called with %q, want %q", written, "new cover")
	}
}

func TestCoverFileWriteEmptyDoesNotCallWriteCover(t *testing.T) {
	called := false
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("original"), nil
		},
		writeCoverFn: func(b *model.Book, data []byte) error {
			called = true
			return nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	fid := uint64(1)
	if err := cf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if err := cf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if called {
		t.Error("WriteCover should not be called when no data was written")
	}
}

func TestCoverFilePerFidBuffers(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("shared"), nil
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	fid1, fid2 := uint64(1), uint64(2)
	cf.Open(fid1, proto.Mode(0))
	cf.Open(fid2, proto.Mode(0))

	cf.Write(fid1, 0, []byte("fid1 data"))

	// Reads return the Open snapshot regardless of writes.
	data1, _ := cf.Read(fid1, 0, 20)
	data2, _ := cf.Read(fid2, 0, 20)
	if string(data1) != "shared" {
		t.Errorf("fid1 read = %q, want %q", data1, "shared")
	}
	if string(data2) != "shared" {
		t.Errorf("fid2 read = %q, want %q", data2, "shared")
	}
}

func TestCoverFileWriteErrorPassesThrough(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		extractCoverFn: func(b *model.Book) ([]byte, error) {
			return []byte("original"), nil
		},
		writeCoverFn: func(b *model.Book, data []byte) error {
			return errTest
		},
	}
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), lib, book)

	fid := uint64(1)
	cf.Open(fid, proto.Mode(0))
	cf.Write(fid, 0, []byte("data"))

	err := cf.Close(fid)
	if err != errTest {
		t.Errorf("Close error = %v, want %v", err, errTest)
	}
}

func TestCoverFileNotOpenRead(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	cf := newCoverFile(f.NewStat("cover.jpg", "glenda", "glenda", 0644), fakeLib{}, book)

	_, err := cf.Read(42, 0, 10)
	if err == nil {
		t.Error("expected error reading from unopened fid")
	}
}
