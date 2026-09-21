package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
)

func TestPruneEmptyNoOpForMissingChild(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	g.pruneEmpty("nonexistent")
}

func TestPruneEmptyNoOpForNonEmptyDir(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	child := newBookListDir(newDirStat(g.f, "child"))
	g.StaticDir.AddChild(child)

	// Add a grandchild so the dir is not empty.
	grandchild := newBookListDir(newDirStat(g.f, "grandchild"))
	child.AddChild(grandchild)

	g.pruneEmpty("child")
	fstest.HasChild(t, g, "child")
}
