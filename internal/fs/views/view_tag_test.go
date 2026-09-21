package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
)

func TestByTagDirTagWithSlash(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"a/b"}
	reg.Add(wrapBook(b))

	fstest.HasChild(t, d, "a-b")

	// A '/' is not a legal 9P name character.
	fstest.NoChild(t, d, "a/b")
}

func TestByTagDirRemoveWithSlashTag(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"x/y"}
	reg.Add(wrapBook(b))
	reg.Remove(1)

	fstest.NoChild(t, d, "x-y")
}
