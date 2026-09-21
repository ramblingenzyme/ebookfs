package vfile

import (
	"bytes"
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func newTestReadAtFile(t *testing.T, data string) *ReadAtFile {
	t.Helper()
	stat := NewStat(util.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) {
		return &mock.EpubReader{Reader: bytes.NewReader([]byte(data))}, nil
	})
	return &raf
}

func TestReadAtFileReadClamps(t *testing.T) {
	fid := fstest.Fid(t, newTestReadAtFile(t, "hello world"), 1)
	fid.Open(proto.Mode(0))

	if got := fid.Read(6, 5); got != "world" {
		t.Errorf("Read(6,5) = %q, want %q", got, "world")
	}

	// Reading at EOF returns no bytes and swallows io.EOF.
	if got := fid.Read(100, 5); got != "" {
		t.Errorf("Read at EOF = %q, want no bytes", got)
	}
}

func TestReadAtFileReadUnopenedErrors(t *testing.T) {
	fstest.Fid(t, newTestReadAtFile(t, "data"), 42).WantReadError()
}

func TestReadAtFileOpenPropagatesError(t *testing.T) {
	stat := NewStat(util.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) { return nil, util.ErrTest })
	if err := raf.Open(1, proto.Mode(0)); err != util.ErrTest {
		t.Errorf("Open error = %v, want %v", err, util.ErrTest)
	}
}

func TestReadAtFilePerFidIsolation(t *testing.T) {
	raf := newTestReadAtFile(t, "shared")
	fid1, fid2 := fstest.Fid(t, raf, 1), fstest.Fid(t, raf, 2)
	fid1.Open(proto.Mode(0))
	fid2.Open(proto.Mode(0))

	fid1.Close()
	if got := fid2.Read(0, 6); got != "shared" {
		t.Errorf("fid2 read = %q, want %q", got, "shared")
	}
}

func TestReadAtFileCloseReleasesReader(t *testing.T) {
	r := &mock.EpubReader{Reader: bytes.NewReader([]byte("data"))}
	stat := NewStat(util.NewTestFS(t), "reader", 0444)
	raf := NewReadAtFile(stat, func() (library.EpubReader, error) { return r, nil })

	fid := fstest.Fid(t, &raf, 1)
	fid.Open(proto.Mode(0))
	if r.Closed {
		t.Fatal("reader should not be closed before Close")
	}
	fid.Close()
	if !r.Closed {
		t.Error("reader should be closed after Close")
	}
}
