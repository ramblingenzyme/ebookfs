package fs

import (
	"testing"
)

func TestNewFS(t *testing.T) {
	f, root := newFS()
	if f == nil {
		t.Fatal("newFS() returned nil FS")
	}
	if root == nil {
		t.Fatal("newFS() returned nil root")
	}
	if root.Stat().Name != "/" {
		t.Errorf("root name = %q, want %q", root.Stat().Name, "/")
	}
}
