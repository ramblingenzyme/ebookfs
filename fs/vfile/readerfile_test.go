package vfile

import (
	"bytes"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func testReaderFile(t *testing.T, exp library.Exporter) *ReaderFile {
	t.Helper()
	f := testutil.NewTestFS(t)
	book := testutil.MakeBook(1, "Test", "Author")
	book.EpubFilename = "test.epub"
	return NewReaderFile(NewStat(f, "test.epub", 0444), exp, testutil.Fixed(book))
}

// Read/open/close semantics are covered by the readAtFile base tests in
// basefile_test.go. These tests cover readerFile's own surface: that it wires
// the Exporter for reads and reports the export size live from Stat.

func TestReaderFileOpenRead(t *testing.T) {
	rf := testReaderFile(t, libfake.Exporter{
		OpenFn: func(b *model.Book) (library.EpubReader, error) {
			return &libfake.EpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
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
	rf := testReaderFile(t, libfake.Exporter{
		SizeFn: func(b *model.Book) (int64, bool) { return 42, true },
	})

	s := rf.Stat()
	if s.Length != 42 {
		t.Errorf("Stat.Length = %d, want 42", s.Length)
	}
}

func TestReaderFileStatFallbackToZero(t *testing.T) {
	rf := testReaderFile(t, libfake.Exporter{
		SizeFn: func(b *model.Book) (int64, bool) { return 0, false },
	})

	s := rf.Stat()
	if s.Length != 0 {
		t.Errorf("Stat.Length for cold book = %d, want 0", s.Length)
	}
}
