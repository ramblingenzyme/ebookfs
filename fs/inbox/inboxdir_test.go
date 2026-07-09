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
	d := NewInboxDir(f)

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
	lib := libfake.Lib{}
	cf := InboxCreateFile(lib, nil)

	dir := NewInboxDir(f)
	file, err := cf(f, dir, "glenda", "test.epub", 0644, 0)
	if err != nil {
		t.Fatalf("InboxCreateFile: %v", err)
	}
	if file == nil {
		t.Fatal("InboxCreateFile returned nil file")
	}

	// File should be added as a child of the inbox dir.
	if _, ok := dir.Children()["test.epub"]; !ok {
		t.Error("inbox dir should contain 'test.epub'")
	}
}

func TestInboxCreateFile_WrongParent(t *testing.T) {
	f := testutil.NewTestFS(t)
	lib := libfake.Lib{}
	cf := InboxCreateFile(lib, nil)

	// Pass a plain StaticDir as parent instead of *InboxDir.
	parent := fs.NewStaticDir(vfile.NewStat(f, "wrong", 0755|proto.DMDIR))
	_, err := cf(f, parent, "glenda", "test.epub", 0644, 0)
	if err == nil {
		t.Fatal("expected error for non-InboxDir parent")
	}
	if err.Error() != "not under inbox" {
		t.Errorf("got error %q, want %q", err.Error(), "not under inbox")
	}
}
