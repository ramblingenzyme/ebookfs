package book

import (
	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// newTestBookDir builds a BookDir over a fresh FS with a no-op edit callback —
// the decoupled constructor needs no registry.
func newTestBookDir(t *testing.T, b *library.Book) *BookDir {
	t.Helper()
	return NewBookDir(testutil.NewTestFS(t), libfake.Lib{}, func(int64, model.Edits) error { return nil }, b)
}

func TestNewBookDirCreatesCoverChild(t *testing.T) {
	b := bookmodel.MakeMutableBook(1, "Has Cover", "Author")
	b.CoverPath = "OEBPS/cover.jpg"

	d := newTestBookDir(t, bookmodel.NewImmutableBook(b))

	if _, ok := d.Children()["cover.jpg"]; !ok {
		t.Error("BookDir should contain 'cover.jpg' when CoverPath is set")
	}
}

func TestNewBookDirNoCoverWhenEmpty(t *testing.T) {
	b := bookmodel.MakeMutableBook(1, "No Cover", "Author")
	b.CoverPath = ""

	d := newTestBookDir(t, bookmodel.NewImmutableBook(b))

	if _, ok := d.Children()["cover.jpg"]; ok {
		t.Error("BookDir should not contain 'cover.jpg' when CoverPath is empty")
	}
}

func TestBookDirStatReportsTitle(t *testing.T) {
	d := newTestBookDir(t, bookmodel.MakeBook(1, "My Title", "Author"))

	s := d.Stat()
	if s.Name != "My Title" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "My Title")
	}
}

func TestBookDirHasIDChild(t *testing.T) {
	d := newTestBookDir(t, bookmodel.MakeBook(1, "Test", "Author"))

	child := d.Children()["id"]
	if child == nil {
		t.Fatal("BookDir should have 'id' child")
	}
	if _, ok := child.(*fs.StaticFile); !ok {
		t.Errorf("'id' child should be a StaticFile")
	}
}
