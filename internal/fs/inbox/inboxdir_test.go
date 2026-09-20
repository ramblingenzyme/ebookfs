package inbox

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/fs/vfile"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
)

func TestNewInboxDir(t *testing.T) {
	f := util.NewTestFS(t)
	d := NewInboxDir(f, mock.Ingester{}, nil)

	s := d.Stat()
	if s.Name != "inbox" {
		t.Errorf("InboxDir name = %q, want %q", s.Name, "inbox")
	}
	if s.Mode&proto.DMDIR == 0 {
		t.Error("InboxDir should have DMDIR flag set")
	}
}

func TestInboxCreateFile_WrongParent(t *testing.T) {
	f := util.NewTestFS(t)

	// A plain StaticDir implements no Creator. Built with the mode that marks a
	// directory as accepting creates, so this also pins that DispatchCreate
	// decides on the type and never on the permission bits.
	parent := fs.NewStaticDir(newStat(f, "wrong", creatableDir))
	_, err := vfile.DispatchCreate(f, parent, "glenda", "test.epub", 0644, 0)
	if err == nil {
		t.Fatal("expected error for non-creatable parent")
	}
	if err.Error() != "cannot create files here" {
		t.Errorf("got error %q, want %q", err.Error(), "cannot create files here")
	}
}
