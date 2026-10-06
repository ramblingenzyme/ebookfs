// These scenarios pin the library operation → hook → registry path: deletes
// forget the registered BookDir, and edits bracket snapshot replacement with
// view callbacks that observe the old and new titles.
package registry

import (
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func TestLibraryDeleteForgetsRegistryEntry(t *testing.T) {
	lib, reg, view := newHookedRegistry(t)
	b := ingestRegistryBook(t, lib, "Delete Me")
	if _, ok := reg.books[b.ID()]; !ok {
		t.Fatal("ingested book is missing from registry")
	}
	view.added = nil
	view.removed = nil

	if err := lib.Delete(b.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	if _, ok := reg.books[b.ID()]; ok {
		t.Errorf("registry still holds deleted book %d", b.ID())
	}
	if !slices.Equal(view.removed, []string{"Delete Me"}) {
		t.Errorf("view removed titles = %v, want [Delete Me]", view.removed)
	}
}

func TestLibraryEditViewsSeeOldThenNewSnapshot(t *testing.T) {
	lib, _, view := newHookedRegistry(t)
	b := ingestRegistryBook(t, lib, "Old Title")
	view.added = nil
	view.removed = nil

	newTitle := "New Title"
	if _, err := lib.Edit(b.ID(), library.Edits{Title: &newTitle}); err != nil {
		t.Fatalf("Edit: %v", err)
	}

	if !slices.Equal(view.removed, []string{"Old Title"}) {
		t.Errorf("Remove observed titles %v, want old snapshot [Old Title]", view.removed)
	}
	if !slices.Equal(view.added, []string{"New Title"}) {
		t.Errorf("Add observed titles %v, want new snapshot [New Title]", view.added)
	}
}

func newHookedRegistry(t *testing.T) (*library.Library, *BookRegistry, *titleView) {
	t.Helper()
	lib, err := library.Open(library.Config(util.TestConfig(t)))
	if err != nil {
		t.Fatalf("Open library: %v", err)
	}
	t.Cleanup(func() { _ = lib.Close() })

	reg := NewBookRegistry(util.NewTestFS(t), lib)
	view := &titleView{}
	reg.AddView(view)
	lib.AddHook(reg)
	return lib, reg, view
}

func ingestRegistryBook(t *testing.T, lib *library.Library, title string) *library.Book {
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(util.BuildTestEpub(t, title, "Author"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	b, err := h.Ingest()
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}

type titleView struct {
	added   []string
	removed []string
}

func (v *titleView) Add(dir *book.BookDir) {
	v.added = append(v.added, dir.Book().Title())
}

func (v *titleView) Remove(dir *book.BookDir) {
	v.removed = append(v.removed, dir.Book().Title())
}
