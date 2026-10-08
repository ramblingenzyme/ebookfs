// Inbox upload scenario: a real library commit must notify the subscribed
// registry so the new book appears in the served views.
package fs

import (
	"testing"

	go9pfs "github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/inbox"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/fs/views"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestInboxUploadReachesViews(t *testing.T) {
	f, lib, inboxDir, dirs := inboxTree(t)
	data := util.BuildTestEpub(t, "Ingested", "Alice")

	if err := upload(t, f, inboxDir, "book.epub", data); err != nil {
		t.Fatalf("clunk: %v", err)
	}

	fstest.ChildCount(t, dirs["books"], 1)
	fstest.HasChild(t, dirs["books"], "Ingested")
	fstest.HasChild(t, dirs["by-author"], "Alice")
	fstest.NoChild(t, inboxDir, "book.epub")

	books, err := lib.Search(library.Query{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(books) != 1 || books[0].Title() != "Ingested" {
		t.Errorf("library returned %d books, want one Ingested book", len(books))
	}
}

func TestInboxFailedIngestLeavesNoBook(t *testing.T) {
	f, _, inboxDir, dirs := inboxTree(t)

	if err := upload(t, f, inboxDir, "bad.epub", []byte("not an epub")); err == nil {
		t.Fatal("clunk succeeded on an ingest that failed")
	}
	fstest.ChildCount(t, dirs["books"], 0)
}

func TestCreateOutsideTheInboxIsRefused(t *testing.T) {
	f, _, _, dirs := inboxTree(t)

	if _, err := vfile.DispatchCreate(f, dirs["books"], "glenda", "x.epub", 0644, 0); err == nil {
		t.Error("a create under books/ was accepted")
	}
}

func inboxTree(t *testing.T) (*go9pfs.FS, *library.Library, go9pfs.Dir, map[string]go9pfs.Dir) {
	t.Helper()
	lib := openRegistryScenarioLibrary(t)
	f := util.NewTestFS(t)
	reg := registry.NewBookRegistry(f, lib)
	lib.AddHook(reg)
	dirs := map[string]go9pfs.Dir{
		"books":     views.NewAllBooksDir(reg),
		"by-author": views.NewByAuthorDir(reg),
	}
	return f, lib, inbox.NewInboxDir(f, lib), dirs
}

// The clunk is the upload transaction boundary.
func upload(t *testing.T, f *go9pfs.FS, inboxDir go9pfs.Dir, name string, data []byte) error {
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
