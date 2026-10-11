package inbox

import (
	"os"
	"testing"
	"time"

	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Close deadlocked when an InboxFile had a real parent: it held the file's
// lock through DeleteChild, which called SetParent on the removed child and
// tried to take the same lock.
func TestInboxFileCloseWithParentDeadlockRegression(t *testing.T) {
	f := util.NewTestFS(t)
	lib := mock.Ingester{
		IngestFn: func(_ string) (*library.Book, error) {
			return util.MakeBook(42, "Test", "Author"), nil
		},
	}

	dir := NewInboxDir(f, lib)

	file, err := dir.Create(f, "test.epub", 0644, 0)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	fid := fstest.Fid(t, file, 1)
	fid.Open(proto.Mode(0))
	fid.Write(0, "epub data")

	// This used to deadlock. Use a timeout to detect it. CloseErr is the clunk
	// that never fails the test, which is what a goroutine can call.
	done := make(chan error, 1)
	go func() {
		done <- fid.CloseErr()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close deadlocked")
	}

}

func TestInboxFileOpen(t *testing.T) {
	// The library stages an upload in its inbox temp directory, so an
	// unwritable one fails the open.
	t.Run("open create ingest error", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root ignores directory permissions")
		}
		cfg := util.TestConfig(t)
		lib, err := library.Open(library.Config(cfg))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { lib.Close() })
		if err := os.Chmod(cfg.InboxTemp, 0555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(cfg.InboxTemp, 0755) })

		inf := NewInboxFile(util.NewTestFS(t), lib, "test.epub", 0644)
		if err := inf.Open(1, proto.Mode(0)); err == nil {
			t.Fatal("Open succeeded with an unwritable inbox temp directory")
		}
	})

	t.Run("double open rejected", func(t *testing.T) {
		inf := NewInboxFile(util.NewTestFS(t), mock.Ingester{}, "test.epub", 0644)

		fstest.Fid(t, inf, 1).Open(proto.Mode(0))

		err := inf.Open(2, proto.Mode(0))
		if err == nil {
			t.Error("expected error opening already-open inboxFile")
		}
	})

	t.Run("open with fid zero", func(t *testing.T) {
		inf := NewInboxFile(util.NewTestFS(t), mock.Ingester{}, "test.epub", 0644)

		// Open with fid 0, a legal fid that used to be rejected as "already open"
		// because the check was i.fid != 0 instead of i.handle != nil.
		fstest.Fid(t, inf, 0).Open(proto.Mode(0))

		// Second open with any fid must still fail.
		err := inf.Open(1, proto.Mode(0))
		if err == nil {
			t.Error("expected error opening already-open inboxFile with fid 1")
		}
	})

	t.Run("write without open", func(t *testing.T) {
		inf := NewInboxFile(util.NewTestFS(t), mock.Ingester{}, "test.epub", 0644)

		_, err := inf.Write(1, 0, []byte("data"))
		if err == nil {
			t.Error("expected error writing to unopened inboxFile")
		}
	})

	t.Run("close without open", func(t *testing.T) {
		inf := NewInboxFile(util.NewTestFS(t), mock.Ingester{}, "test.epub", 0644)

		fstest.Fid(t, inf, 1).Close()
	})

	t.Run("reopen after close", func(t *testing.T) {
		ingestCount := 0
		f := util.NewTestFS(t)
		lib := mock.Ingester{
			IngestFn: func(_ string) (*library.Book, error) {
				ingestCount++
				return util.MakeBook(int64(ingestCount), "Test", "Author"), nil
			},
		}

		inf := NewInboxFile(f, lib, "test.epub", 0644)

		fstest.Fid(t, inf, 1).Set(proto.Mode(0), "first")

		if ingestCount != 1 {
			t.Fatalf("expected 1 ingest, got %d", ingestCount)
		}

		fstest.Fid(t, inf, 2).Set(proto.Mode(0), "second")

		if ingestCount != 2 {
			t.Errorf("expected 2 ingests, got %d", ingestCount)
		}
	})
}
