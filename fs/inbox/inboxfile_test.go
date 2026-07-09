package inbox

// Untested:
//   - inboxFile.Close small edge-case branches (parent not a ModDir, etc.)

import (
	"errors"
	"testing"
	"time"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

func TestInboxFileOpenCreateIngestError(t *testing.T) {
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{
		CreateIngestFn: func() (library.IngestHandle, error) {
			return nil, errors.New("CreateIngest failed")
		},
	}
	inf := NewInboxFile(f, lib, "test.epub", 0644, nil)

	err := inf.Open(1, proto.Mode(0))
	if err == nil {
		t.Fatal("expected error from CreateIngest")
	}
}

func TestInboxFileOpenWriteCloseIngests(t *testing.T) {
	ingested := make(chan *model.Book, 1)
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{
		IngestFn: func(_ string) (*model.Book, error) {
			return testutil.MakeBook(42, "Ingested", "Author"), nil
		},
	}

	inf := NewInboxFile(f, lib, "test.epub", 0644, func(b *model.Book) {
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
	f := testutil.NewTestFS(t)
	inf := NewInboxFile(f, libfake.Lib{}, "test.epub", 0644, nil)

	fid1, fid2 := uint64(1), uint64(2)
	if err := inf.Open(fid1, proto.Mode(0)); err != nil {
		t.Fatalf("Open fid1: %v", err)
	}

	err := inf.Open(fid2, proto.Mode(0))
	if err == nil {
		t.Error("expected error opening already-open inboxFile")
	}
}

func TestInboxFileOpenWithFidZero(t *testing.T) {
	f := testutil.NewTestFS(t)
	inf := NewInboxFile(f, libfake.Lib{}, "test.epub", 0644, nil)

	// Open with fid 0 — a legal fid that used to be rejected as "already open"
	// because the check was i.fid != 0 instead of i.handle != nil.
	if err := inf.Open(0, proto.Mode(0)); err != nil {
		t.Fatalf("Open with fid 0: %v", err)
	}

	// Second open with any fid must still fail.
	err := inf.Open(1, proto.Mode(0))
	if err == nil {
		t.Error("expected error opening already-open inboxFile with fid 1")
	}
}

func TestInboxFileWriteWithoutOpen(t *testing.T) {
	f := testutil.NewTestFS(t)
	inf := NewInboxFile(f, libfake.Lib{}, "test.epub", 0644, nil)

	_, err := inf.Write(1, 0, []byte("data"))
	if err == nil {
		t.Error("expected error writing to unopened inboxFile")
	}
}

func TestInboxFileCloseWithoutOpen(t *testing.T) {
	f := testutil.NewTestFS(t)
	inf := NewInboxFile(f, libfake.Lib{}, "test.epub", 0644, nil)

	err := inf.Close(1)
	if err != nil {
		t.Errorf("Close unopened inboxFile: %v", err)
	}
}

func TestInboxFileIngestErrorReturnsError(t *testing.T) {
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{
		IngestFn: func(_ string) (*model.Book, error) {
			return nil, testutil.ErrTest
		},
	}

	inf := NewInboxFile(f, lib, "test.epub", 0644, nil)

	fid := uint64(1)
	if err := inf.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := inf.Write(fid, 0, []byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	err := inf.Close(fid)
	if err != testutil.ErrTest {
		t.Errorf("Close error = %v, want %v", err, testutil.ErrTest)
	}
}

func TestInboxFileReopenAfterClose(t *testing.T) {
	ingestCount := 0
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{
		IngestFn: func(_ string) (*model.Book, error) {
			ingestCount++
			return testutil.MakeBook(int64(ingestCount), "Test", "Author"), nil
		},
	}

	noop := func(b *model.Book) {}
	inf := NewInboxFile(f, lib, "test.epub", 0644, noop)

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

// TestInboxFileCloseWithParentDeadlockRegression verifies that Close completes
// when the inboxFile has a real parent directory. This is a regression test for
// a deadlock where Close held the file's lock while calling DeleteChild, which
// in turn called SetParent on the removed child, trying to acquire the same lock.
func TestInboxFileCloseWithParentDeadlockRegression(t *testing.T) {
	ingested := make(chan *model.Book, 1)
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{
		IngestFn: func(_ string) (*model.Book, error) {
			return testutil.MakeBook(42, "Test", "Author"), nil
		},
	}

	dir := NewInboxDir(f)
	cf := InboxCreateFile(lib, func(b *model.Book) {
		ingested <- b
	})

	file, err := cf(f, dir, "glenda", "test.epub", 0644, 0)
	if err != nil {
		t.Fatalf("inboxCreateFile: %v", err)
	}

	fid := uint64(1)
	if err := file.Open(fid, proto.Mode(0)); err != nil {
		t.Fatalf("Open: %v", err)
	}

	if _, err := file.Write(fid, 0, []byte("epub data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// This used to deadlock. Use a timeout to detect it.
	done := make(chan error, 1)
	go func() {
		done <- file.Close(fid)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked")
	}

	// Verify ingest was called
	select {
	case b := <-ingested:
		if b.Meta.ID != 42 {
			t.Errorf("ingested book id = %d, want 42", b.Meta.ID)
		}
	default:
		t.Fatal("onIngest was not called")
	}
}
