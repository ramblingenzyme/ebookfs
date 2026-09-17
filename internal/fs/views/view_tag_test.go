package views

import (
	"testing"
)

func TestByTagDirTagWithSlash(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"a/b"}
	reg.Add(wrapBook(b))

	if _, ok := d.Children()["a_b"]; !ok {
		t.Fatalf("by-tag should have 'a_b' subdir for tag 'a/b', got: %v", dirChildNames(d))
	}
	if _, ok := d.Children()["a/b"]; ok {
		t.Error("by-tag should NOT have 'a/b' subdir (slash not valid in 9P names)")
	}
}

func TestByTagDirRemoveWithSlashTag(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"x/y"}
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if _, ok := d.Children()["x_y"]; ok {
		t.Error("tag subdir should be pruned after remove")
	}
}
