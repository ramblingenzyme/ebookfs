package inbox

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

func TestNewInboxDir(t *testing.T) {
	f := testutil.NewTestFS(t)
	d := NewInboxDir(f, libfake.Lib{}, nil)

	s := d.Stat()
	if s.Name != "inbox" {
		t.Errorf("InboxDir name = %q, want %q", s.Name, "inbox")
	}
	if s.Mode&proto.DMDIR == 0 {
		t.Error("InboxDir should have DMDIR flag set")
	}
}

func TestInboxCreateFile_Success(t *testing.T) {
	f := testutil.NewTestFS(t)
	dir := NewInboxDir(f, libfake.Lib{}, nil)

	file, err := vfile.DispatchCreate(f, dir, "glenda", "test.epub", 0644, 0)
	if err != nil {
		t.Fatalf("DispatchCreate: %v", err)
	}
	if file == nil {
		t.Fatal("DispatchCreate returned nil file")
	}

	// File should be added as a child of the inbox dir.
	if _, ok := dir.Children()["test.epub"]; !ok {
		t.Error("inbox dir should contain 'test.epub'")
	}
}

func TestInboxCreateFile_WrongParent(t *testing.T) {
	f := testutil.NewTestFS(t)

	// Pass a plain StaticDir (no Creator implementation) as the parent.
	parent := fs.NewStaticDir(newStat(f, "wrong", 0755|proto.DMDIR))
	_, err := vfile.DispatchCreate(f, parent, "glenda", "test.epub", 0644, 0)
	if err == nil {
		t.Fatal("expected error for non-creatable parent")
	}
	if err.Error() != "cannot create files here" {
		t.Errorf("got error %q, want %q", err.Error(), "cannot create files here")
	}
}
