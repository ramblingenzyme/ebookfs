package inbox

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

func TestNewInboxDir(t *testing.T) {
	f := testutil.NewTestFS(t)
	d := NewInboxDir(f, ingester{}, nil)

	s := d.Stat()
	if s.Name != "inbox" {
		t.Errorf("InboxDir name = %q, want %q", s.Name, "inbox")
	}
	if s.Mode&proto.DMDIR == 0 {
		t.Error("InboxDir should have DMDIR flag set")
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
