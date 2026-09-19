package book

import (
	"bytes"
	"os"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/libtest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// epubFile's own surface, on top of the readAtFile semantics its base test
// owns: it wires lib.OpenEpub for reads and reports name/size from the book
// snapshot in Stat.

func newTestEpubFile(t *testing.T, name string, lib ContentReader, get func() *library.Book) *epubFile {
	t.Helper()
	return newEpubFile(newStat(testutil.NewTestFS(t), name, 0444), lib, get)
}

func TestEpubFileOpenRead(t *testing.T) {
	lib := libtest.ContentReader{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return &libtest.EpubReader{Reader: bytes.NewReader([]byte("epub content"))}, nil
		},
	}
	ef := newTestEpubFile(t, "test.epub", lib, testutil.Fixed(testutil.MakeBook(1, "Test", "Author")))

	fid := fstest.Fid(t, ef, 1)
	if got := fid.Get(proto.Mode(0), 20); got != "epub content" {
		t.Errorf("Read = %q, want %q", got, "epub content")
	}
	fid.Close()
}

func TestEpubFileStatSize(t *testing.T) {
	book := testutil.MakeMutableBook(1, "Test", "Author")
	book.EpubPath = "/nonexistent/test.epub"
	ef := newTestEpubFile(t, "test.epub", libtest.ContentReader{}, testutil.Fixed(testutil.WrapBook(book)))

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
	ef := newTestEpubFile(t, "test.epub", libtest.ContentReader{}, func() *library.Book { return nil })

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

	book := testutil.MakeMutableBook(1, "Test", "Author")
	book.EpubPath = path
	book.EpubSize = int64(len(content))

	ef := newTestEpubFile(t, "book.epub", libtest.ContentReader{}, testutil.Fixed(testutil.WrapBook(book)))

	s := ef.Stat()
	if s.Name != "book.epub" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "book.epub")
	}
	if s.Length != uint64(len(content)) {
		t.Errorf("Stat.Length = %d, want %d", s.Length, len(content))
	}
}
