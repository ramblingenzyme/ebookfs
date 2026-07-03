package fs

import (
	"bytes"
	"os"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestEpubFileOpenReadClose(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("epub content"))}, nil
		},
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	fid := uint64(1)
	if err := ef.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ef.Read(fid, 0, 20)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "epub content" {
		t.Errorf("Read = %q, want %q", data, "epub content")
	}

	if err := ef.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestEpubFileReadPartial(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("epub content"))}, nil
		},
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	fid := uint64(1)
	if err := ef.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ef.Read(fid, 3, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "b con" {
		t.Errorf("Read(3,5) = %q, want %q", data, "b con")
	}
}

func TestEpubFileReadAtEOF(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("hi"))}, nil
		},
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	fid := uint64(1)
	if err := ef.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := ef.Read(fid, 10, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read at EOF = %d bytes, want 0", len(data))
	}
}

func TestEpubFileCloseReleasesReader(t *testing.T) {
	f := newTestFS(t)
	r := &fakeEpubReader{Reader: bytes.NewReader([]byte("data"))}
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) { return r, nil },
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	fid := uint64(1)
	if err := ef.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r.closed {
		t.Fatal("reader should not be closed before Close")
	}

	if err := ef.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !r.closed {
		t.Error("reader should be closed after Close")
	}
}

func TestEpubFileStatSize(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "test.epub"
	book.EpubPath = "/nonexistent/test.epub"
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), fakeLib{}, book)

	s := ef.Stat()
	if s.Name != "test.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "test.epub")
	}
	// Length should be 0 since the file doesn't exist and os.Stat fails
	if s.Length != 0 {
		t.Errorf("Stat.Length = %d, want 0 for nonexistent file", s.Length)
	}
}

func TestEpubFileOpenError(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return nil, errTest
		},
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	err := ef.Open(1, proto.Mode(0))
	if err != errTest {
		t.Errorf("Open error = %v, want %v", err, errTest)
	}
}

func TestEpubFileMultiFid(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		openEpubFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("content"))}, nil
		},
	}
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), lib, book)

	fid1, fid2 := uint64(1), uint64(2)
	ef.Open(fid1, proto.Mode(0))
	ef.Open(fid2, proto.Mode(0))

	data1, _ := ef.Read(fid1, 0, 20)
	data2, _ := ef.Read(fid2, 0, 20)
	if string(data1) != "content" || string(data2) != "content" {
		t.Errorf("both fids should read the same data")
	}

	ef.Close(fid1)
	_, err := ef.Read(fid2, 0, 20)
	if err != nil {
		t.Fatalf("Read fid2 after fid1 closed: %v", err)
	}
}

func TestEpubFileNotOpenRead(t *testing.T) {
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	ef := newEpubFile(f.NewStat("test.epub", "glenda", "glenda", 0444), fakeLib{}, book)

	_, err := ef.Read(42, 0, 10)
	if err == nil {
		t.Error("expected error reading from unopened fid")
	}
}

func TestEpubFileStatWithRealFile(t *testing.T) {
	f := newTestFS(t)
	content := []byte("fake epub content")
	path := t.TempDir() + "/book.epub"
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "book.epub"
	book.EpubPath = path

	ef := newEpubFile(f.NewStat("book.epub", "glenda", "glenda", 0444), fakeLib{}, book)

	s := ef.Stat()
	if s.Name != "book.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "book.epub")
	}
	if s.Length != uint64(len(content)) {
		t.Errorf("Stat.Length = %d, want %d", s.Length, len(content))
	}
}
