package fs

import (
	"bytes"
	"errors"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

var errTest = errors.New("test error")

func testReaderFile(t *testing.T, exp library.Exporter) *readerFile {
	t.Helper()
	f := newTestFS(t)
	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "test.epub"
	return newReaderFile(f.NewStat("test.epub", "glenda", "glenda", 0444), exp, fixed(book))
}

func TestReaderFileOpenReadClose(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
		},
	})

	fid := uint64(1)
	if err := rf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := rf.Read(fid, 0, 20)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "hello epub" {
		t.Errorf("Read = %q, want %q", data, "hello epub")
	}

	if err := rf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestReaderFileReadPartial(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
		},
	})

	fid := uint64(1)
	if err := rf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := rf.Read(fid, 2, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(data) != "llo e" {
		t.Errorf("Read(2,5) = %q, want %q", data, "llo e")
	}
}

func TestReaderFileReadAtEOF(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("hi"))}, nil
		},
	})

	fid := uint64(1)
	if err := rf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	data, err := rf.Read(fid, 10, 5)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(data) != 0 {
		t.Errorf("Read at EOF = %d bytes, want 0", len(data))
	}
}

func TestReaderFileCloseReleasesReader(t *testing.T) {
	r := &fakeEpubReader{Reader: bytes.NewReader([]byte("data"))}
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) { return r, nil },
	})

	fid := uint64(1)
	if err := rf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if r.closed {
		t.Fatal("reader should not be closed before Close")
	}

	if err := rf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !r.closed {
		t.Error("reader should be closed after Close")
	}
}

func TestReaderFileStatReportsSize(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		sizeFn: func(b *model.Book) (int64, bool) { return 42, true },
	})

	s := rf.Stat()
	if s.Length != 42 {
		t.Errorf("Stat.Length = %d, want 42", s.Length)
	}
}

func TestReaderFileStatFallbackToZero(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		sizeFn: func(b *model.Book) (int64, bool) { return 0, false },
	})

	s := rf.Stat()
	if s.Length != 0 {
		t.Errorf("Stat.Length for cold book = %d, want 0", s.Length)
	}
}

func TestReaderFileMultiFid(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) {
			return &fakeEpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
		},
	})

	fid1, fid2 := uint64(1), uint64(2)
	if err := rf.Open(fid1, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid1: %v", err)
	}
	if err := rf.Open(fid2, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid2: %v", err)
	}

	data1, _ := rf.Read(fid1, 0, 20)
	data2, _ := rf.Read(fid2, 0, 20)
	if string(data1) != "hello epub" || string(data2) != "hello epub" {
		t.Errorf("both fids should read the same data")
	}

	rf.Close(fid1)
	data2, err := rf.Read(fid2, 0, 20)
	if err != nil {
		t.Fatalf("Read fid2 after fid1 closed: %v", err)
	}
	if string(data2) != "hello epub" {
		t.Errorf("fid2 should still work after fid1 closed")
	}
}

func TestReaderFileOpenError(t *testing.T) {
	rf := testReaderFile(t, testExporter{
		openFn: func(b *model.Book) (library.EpubReader, error) {
			return nil, errTest
		},
	})

	err := rf.Open(1, proto.Mode(0))
	if err != errTest {
		t.Errorf("Open error = %v, want %v", err, errTest)
	}
}

func TestReaderFileNotOpenRead(t *testing.T) {
	rf := testReaderFile(t, testExporter{})

	_, err := rf.Read(42, 0, 10)
	if err == nil {
		t.Error("expected error reading from unopened fid")
	}
}
