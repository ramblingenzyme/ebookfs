// An upload driven the way a 9P client drives one: create a file under inbox/,
// write to it, clunk, and expect the book in the served tree. The create goes
// through the FS-wide dispatch hook rather than straight to InboxDir, since
// routing a create to the right directory is part of what this pins.
//
// The unit tests in fs/inbox drive InboxFile with a fake on both ends. Nothing
// there proves an ingested book reaches the views.

package fs

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/inbox"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library"
)

// inboxTree wires a real inbox onto a real registry and views. ingested is what
// the library hands back, or nil to make the ingest fail.
func inboxTree(t *testing.T, ingested *bookmodel.Book) (*fs.FS, fs.Dir, map[string]fs.Dir) {
	t.Helper()
	f := newTestFS(t)

	lib := libfake.Lib{
		IngestFn: func(string) (*library.Book, error) {
			if ingested == nil {
				return nil, errTest
			}
			return testutil.WrapBook(ingested), nil
		},
	}

	reg := registry.NewBookRegistry(f, lib)
	dirs := map[string]fs.Dir{
		"books":     views.NewAllBooksDir(reg),
		"by-author": views.NewByAuthorDir(reg),
	}
	return f, inbox.NewInboxDir(f, lib, reg.Add), dirs
}

// upload copies data into inbox/name the way a client would: dispatch a create,
// open, write, clunk. The clunk is the transaction boundary.
func upload(t *testing.T, f *fs.FS, inboxDir fs.Dir, name string, data []byte) error {
	t.Helper()
	file, err := vfile.DispatchCreate(f, inboxDir, "glenda", name, 0644, 0)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	fid := fstest.Fid(t, file, 1)
	fid.Open(proto.Mode(0))
	fid.Write(0, string(data))
	return fid.CloseErr()
}

func TestInboxUploadReachesTheViews(t *testing.T) {
	f, inboxDir, dirs := inboxTree(t, makeBook(1, "Ingested", "Alice"))

	if err := upload(t, f, inboxDir, "book.epub", []byte("epub-bytes")); err != nil {
		t.Fatalf("clunk: %v", err)
	}

	fstest.ChildCount(t, dirs["books"], 1)
	fstest.HasChild(t, dirs["books"], "Ingested")
	fstest.HasChild(t, dirs["by-author"], "Alice")
	fstest.NoChild(t, inboxDir, "book.epub")
}

// A failed ingest must surface on the clunk and leave the tree empty. The
// client's only signal is that error, and a book half-added to the views would
// be worse than one rejected.
func TestInboxFailedIngestLeavesNoBook(t *testing.T) {
	f, inboxDir, dirs := inboxTree(t, nil)

	if err := upload(t, f, inboxDir, "bad.epub", []byte("not an epub")); err == nil {
		t.Fatal("clunk succeeded on an ingest that failed")
	}
	fstest.ChildCount(t, dirs["books"], 0)
}

// Every view directory is the same case: the tree is derived from the library,
// so a create anywhere but the inbox has nowhere to land.
func TestCreateOutsideTheInboxIsRefused(t *testing.T) {
	f, _, dirs := inboxTree(t, makeBook(1, "Ingested", "Alice"))

	if _, err := vfile.DispatchCreate(f, dirs["books"], "glenda", "x.epub", 0644, 0); err == nil {
		t.Error("a create under books/ was accepted")
	}
}
