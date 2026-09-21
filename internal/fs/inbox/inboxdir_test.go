package inbox

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
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
