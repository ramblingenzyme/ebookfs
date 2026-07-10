package book

import (
	"bytes"
	"os"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// Read/open/close semantics are covered by the readAtFile base tests in
// basefile_test.go. These tests cover epubFile's own surface: that it wires
// lib.OpenEpub for reads and reports name/size from the book snapshot in Stat.

func newTestEpubFile(t *testing.T, name string, lib libfake.Lib, get func() *model.Book) *epubFile {
	t.Helper()
	return newEpubFile(newStat(testutil.NewTestFS(t), name, 0444), lib, get)
}

func TestEpubFileOpenRead(t *testing.T) {
	lib := libfake.Lib{
		OpenEpubFn: func(_ int64) (library.EpubReader, error) {
			return &libfake.EpubReader{Reader: bytes.NewReader([]byte("epub content"))}, nil
		},
	}
	ef := newTestEpubFile(t, "test.epub", lib, testutil.Fixed(testutil.MakeBook(1, "Test", "Author")))

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

func TestEpubFileStatSize(t *testing.T) {
	book := testutil.MakeBook(1, "Test", "Author")
	book.EpubFilename = "test.epub"
	book.EpubPath = "/nonexistent/test.epub"
	ef := newTestEpubFile(t, "test.epub", libfake.Lib{}, testutil.Fixed(book))

	s := ef.Stat()
	if s.Name != "test.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "test.epub")
	}
	// Length should be 0 since the book snapshot carries no EpubSize.
	if s.Length != 0 {
		t.Errorf("Stat.Length = %d, want 0 for nonexistent file", s.Length)
	}
}

func TestEpubFileStatNilBook(t *testing.T) {
	ef := newTestEpubFile(t, "test.epub", libfake.Lib{}, func() *model.Book { return nil })

	s := ef.Stat()
	if s.Name != "test.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "test.epub")
	}
	if s.Length != 0 {
		t.Errorf("Stat.Length = %d, want 0", s.Length)
	}
}

func TestEpubFileStatWithRealFile(t *testing.T) {
	content := []byte("fake epub content")
	path := t.TempDir() + "/book.epub"
	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	book := testutil.MakeBook(1, "Test", "Author")
	book.EpubFilename = "book.epub"
	book.EpubPath = path
	book.EpubSize = int64(len(content))

	ef := newTestEpubFile(t, "book.epub", libfake.Lib{}, testutil.Fixed(book))

	s := ef.Stat()
	if s.Name != "book.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "book.epub")
	}
	if s.Length != uint64(len(content)) {
		t.Errorf("Stat.Length = %d, want %d", s.Length, len(content))
	}
}
