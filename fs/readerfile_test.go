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
	return newReaderFile(newStat(f, "test.epub", 0444), exp, fixed(book))
}

// Read/open/close semantics are covered by the readAtFile base tests in
// basefile_test.go. These tests cover readerFile's own surface: that it wires
// the Exporter for reads and reports the export size live from Stat.

func TestReaderFileOpenRead(t *testing.T) {
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
