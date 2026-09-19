package book

import (
	"bytes"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/libtest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

func testReaderFile(t *testing.T, exp Renderer) *ReaderFile {
	t.Helper()
	f := testutil.NewTestFS(t)
	book := testutil.MakeBook(1, "Test", "Author")
	return NewReaderFile(newStat(f, "test.epub", 0444), exp, testutil.Fixed(book))
}

// readerFile's own surface, on top of the readAtFile semantics its base test
// owns: it wires the Renderer for reads and reports the export size live from
// Stat.

func TestReaderFileOpenRead(t *testing.T) {
	rf := testReaderFile(t, libtest.Renderer{
		OpenFn: func(b *library.Book) (library.EpubReader, error) {
			return &libtest.EpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
		},
	})

	fid := fstest.Fid(t, rf, 1)
	if got := fid.Get(proto.Mode(0), 20); got != "hello epub" {
		t.Errorf("Read = %q, want %q", got, "hello epub")
	}
	fid.Close()
}

func TestReaderFileStatReportsSize(t *testing.T) {
	rf := testReaderFile(t, libtest.Renderer{
		SizeFn: func(b *library.Book) (int64, bool) { return 42, true },
	})

	s := rf.Stat()
	if s.Length != 42 {
		t.Errorf("Stat.Length = %d, want 42", s.Length)
	}
}

func TestReaderFileStatFallbackToZero(t *testing.T) {
	rf := testReaderFile(t, libtest.Renderer{
		SizeFn: func(b *library.Book) (int64, bool) { return 0, false },
	})

	s := rf.Stat()
	if s.Length != 0 {
		t.Errorf("Stat.Length for cold book = %d, want 0", s.Length)
	}
}

// Stat must report zero rather than panic when the exporter is absent, the same
// case the read path handles with "exporter not available".
func TestReaderFileStatNilExporter(t *testing.T) {
	rf := testReaderFile(t, nil)

	fstest.StatLength(t, rf, 0)
}
