package fs

// Untested:
//   - inboxFile.Close small edge-case branches (parent not a ModDir, etc.)

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

func TestInboxFileOpenWriteCloseIngests(t *testing.T) {
	ingested := make(chan *model.Book, 1)
	f := newTestFS(t)
	lib := fakeLib{
		ingestFn: func(path string) (*model.Book, error) {
			return makeBook(42, "Ingested", "Author"), nil
		},
	}

	inf := newInboxFile(f, lib, t.TempDir(), "test.epub", 0644, func(b *model.Book) {
		ingested <- b
	})

	fid := uint64(1)
	if err := inf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := inf.Write(fid, 0, []byte("epub data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if err := inf.Close(fid); err != nil {
		t.Fatalf("Close: %v", err)
	}

	select {
	case b := <-ingested:
		if b.Meta.ID != 42 {
			t.Errorf("ingested book id = %d, want 42", b.Meta.ID)
		}
	default:
		t.Fatal("onIngest was not called after close")
	}
}

func TestInboxFileDoubleOpenRejected(t *testing.T) {
	f := newTestFS(t)
	inf := newInboxFile(f, fakeLib{}, t.TempDir(), "test.epub", 0644, nil)

	fid1, fid2 := uint64(1), uint64(2)
	if err := inf.Open(fid1, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid1: %v", err)
	}

	err := inf.Open(fid2, proto.Mode(0))
	if err == nil {
		t.Error("expected error opening already-open inboxFile")
	}
}

func TestInboxFileWriteWithoutOpen(t *testing.T) {
	f := newTestFS(t)
	inf := newInboxFile(f, fakeLib{}, t.TempDir(), "test.epub", 0644, nil)

	_, err := inf.Write(1, 0, []byte("data"))
	if err == nil {
		t.Error("expected error writing to unopened inboxFile")
	}
}

func TestInboxFileCloseWithoutOpen(t *testing.T) {
	f := newTestFS(t)
	inf := newInboxFile(f, fakeLib{}, t.TempDir(), "test.epub", 0644, nil)

	err := inf.Close(1)
	if err != nil {
		t.Errorf("Close unopened inboxFile: %v", err)
	}
}

func TestInboxFileIngestErrorRemovesTempFile(t *testing.T) {
	f := newTestFS(t)
	lib := fakeLib{
		ingestFn: func(path string) (*model.Book, error) {
			return nil, errTest
		},
	}

	inf := newInboxFile(f, lib, t.TempDir(), "test.epub", 0644, nil)

	fid := uint64(1)
	if err := inf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := inf.Write(fid, 0, []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := inf.Close(fid)
	if err != errTest {
		t.Errorf("Close error = %v, want %v", err, errTest)
	}
}

func TestInboxFileReopenAfterClose(t *testing.T) {
	ingestCount := 0
	f := newTestFS(t)
	lib := fakeLib{
		ingestFn: func(path string) (*model.Book, error) {
			ingestCount++
			return makeBook(int64(ingestCount), "Test", "Author"), nil
		},
	}

	noop := func(b *model.Book) {}
	inf := newInboxFile(f, lib, t.TempDir(), "test.epub", 0644, noop)

	// First open/write/close
	fid := uint64(1)
	inf.Open(fid, proto.Mode(0))
	inf.Write(fid, 0, []byte("first"))
	inf.Close(fid)

	if ingestCount != 1 {
		t.Fatalf("expected 1 ingest, got %d", ingestCount)
	}

	// Second open/write/close with different fid
	fid2 := uint64(2)
	inf.Open(fid2, proto.Mode(0))
	inf.Write(fid2, 0, []byte("second"))
	inf.Close(fid2)

	if ingestCount != 2 {
		t.Errorf("expected 2 ingests, got %d", ingestCount)
	}
}
