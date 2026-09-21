package book

import (
	"bytes"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/knusbaum/go9p/proto"
)

func testReaderFile(t *testing.T, exp Renderer) *ReaderFile {
	t.Helper()
	f := util.NewTestFS(t)
	book := util.MakeBook(1, "Test", "Author")
	return NewReaderFile(newStat(f, "test.epub", 0444), exp, util.Fixed(book))
}

func TestReaderFileOpenRead(t *testing.T) {
	rf := testReaderFile(t, mock.Renderer{
		OpenFn: func(b *library.Book) (library.EpubReader, error) {
			return &mock.EpubReader{Reader: bytes.NewReader([]byte("hello epub"))}, nil
		},
	})

	fid := fstest.Fid(t, rf, 1)
	if got := fid.Get(proto.Mode(0), 20); got != "hello epub" {
		t.Errorf("Read = %q, want %q", got, "hello epub")
	}
	fid.Close()
}

func TestReaderFileStatReportsSize(t *testing.T) {
	rf := testReaderFile(t, mock.Renderer{
		SizeFn: func(b *library.Book) (int64, bool) { return 42, true },
	})

	s := rf.Stat()
	if s.Length != 42 {
		t.Errorf("Stat.Length = %d, want 42", s.Length)
	}
}

func TestReaderFileStatFallbackToZero(t *testing.T) {
	rf := testReaderFile(t, mock.Renderer{
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
