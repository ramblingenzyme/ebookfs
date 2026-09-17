package views

import (
	"testing"

	"github.com/knusbaum/go9p/proto"
)

func TestPruneEmptyNoOpForMissingChild(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	g.pruneEmpty("nonexistent")
}

func TestPruneEmptyNoOpForNonEmptyDir(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	child := newBookListDir(newStat(g.f, "child", 0555|proto.DMDIR))
	g.StaticDir.AddChild(child)

	// Add a grandchild so the dir is not empty.
	grandchild := newBookListDir(newStat(g.f, "grandchild", 0555|proto.DMDIR))
	child.AddChild(grandchild)

	g.pruneEmpty("child")
	if _, ok := g.Children()["child"]; !ok {
		t.Error("non-empty child should not be pruned")
	}
}
