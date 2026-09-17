package book

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
)

// coverFile's own surface, on top of the snapshotFile and size-cap semantics
// the base tests own: Stat length from CoverSize, and the per-fid write buffer
// committed to Edit on Close.

func newTestCoverFile(t *testing.T, lib libfake.Lib, edit func(int64, library.Edits) error) *coverFile {
	t.Helper()
	book := testutil.MakeMutableBook(1, "Test", "Author")
	book.CoverSize = 16
	return newCoverFile(newStat(testutil.NewTestFS(t), "cover.jpg", 0644), lib, edit, testutil.Fixed(testutil.WrapBook(book)))
}

func TestCoverFileStatLength(t *testing.T) {
	cf := newTestCoverFile(t, libfake.Lib{}, func(int64, library.Edits) error { return nil })

	fstest.StatLength(t, cf, 16)
}

func TestCoverFileStatLengthNilLib(t *testing.T) {
	f := testutil.NewTestFS(t)
	book := testutil.MakeBook(1, "Test", "Author")
	cf := newCoverFile(newStat(f, "cover.jpg", 0644), nil, func(int64, library.Edits) error { return nil }, testutil.Fixed(book))

	if s := cf.Stat(); s.Length != 0 {
		t.Errorf("Stat().Length with nil lib = %d, want 0", s.Length)
	}
}

func TestCoverFileOpenRead(t *testing.T) {
	lib := libfake.Lib{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return libfake.NewEpubReader(nil, nil, func() ([]byte, error) { return []byte("cover image data"), nil }), nil
		},
	}
	cf := newTestCoverFile(t, lib, func(int64, library.Edits) error { return nil })

	if got := fstest.Fid(t, cf, 1).Get(proto.Mode(0), 50); got != "cover image data" {
		t.Errorf("Read = %q, want %q", got, "cover image data")
	}
}

func TestCoverFileWriteClose(t *testing.T) {
	var written *[]byte
	lib := libfake.Lib{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return libfake.NewEpubReader(nil, nil, func() ([]byte, error) { return []byte("original"), nil }), nil
		},
	}
	cf := newTestCoverFile(t, lib, func(id int64, edits library.Edits) error {
		written = edits.Cover
		return nil
	})

	fstest.Fid(t, cf, 1).Set(proto.Mode(0), "new cover")

	if string(*written) != "new cover" {
		t.Errorf("edit called with %q, want %q", string(*written), "new cover")
	}
}

func TestCoverFileWriteEmptyDoesNotCallEdit(t *testing.T) {
	called := false
	lib := libfake.Lib{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return libfake.NewEpubReader(nil, nil, func() ([]byte, error) { return []byte("original"), nil }), nil
		},
	}
	cf := newTestCoverFile(t, lib, func(int64, library.Edits) error {
		called = true
		return nil
	})

	fid := fstest.Fid(t, cf, 1)
	fid.Open(proto.Mode(0))
	fid.Close()

	if called {
		t.Error("edit should not be called when no data was written")
	}
}

func TestCoverFilePerFidBuffers(t *testing.T) {
	lib := libfake.Lib{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return libfake.NewEpubReader(nil, nil, func() ([]byte, error) { return []byte("shared"), nil }), nil
		},
	}
	cf := newTestCoverFile(t, lib, func(int64, library.Edits) error { return nil })

	fid1, fid2 := fstest.Fid(t, cf, 1), fstest.Fid(t, cf, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))

	fid1.Write(0, "fid1 data")

	// Reads return the Open snapshot regardless of writes to any fid.
	data1, data2 := fid1.Read(0, 20), fid2.Read(0, 20)
	if data1 != "shared" || data2 != "shared" {
		t.Errorf("reads = %q/%q, want both %q", data1, data2, "shared")
	}
}

func TestCoverFileWriteErrorPassesThrough(t *testing.T) {
	lib := libfake.Lib{
		ContentFn: func(_ int64) (library.EpubReader, error) {
			return libfake.NewEpubReader(nil, nil, func() ([]byte, error) { return []byte("original"), nil }), nil
		},
	}
	cf := newTestCoverFile(t, lib, func(int64, library.Edits) error { return testutil.ErrTest })

	fid := fstest.Fid(t, cf, 1)
	fid.Open(proto.Mode(0))
	fid.Write(0, "data")

	if err := fid.WantCloseError(); err != testutil.ErrTest {
		t.Errorf("Close error = %v, want %v", err, testutil.ErrTest)
	}
}
