package fs

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
)

func TestNewBookDirCreatesCoverChild(t *testing.T) {
	f := newTestFS(t)
	reg := newTestRegistry(t, f)
	d := newBookDir(reg, makeBook(1, "Has Cover", "Author"))
	d.Book.CoverPath = "OEBPS/cover.jpg"

	d2 := newBookDir(reg, d.Book)

	if _, ok := d2.Children()["cover.jpg"]; !ok {
		t.Error("bookDir should contain 'cover.jpg' when CoverPath is set")
	}
}

func TestNewBookDirNoCoverWhenEmpty(t *testing.T) {
	f := newTestFS(t)
	reg := newTestRegistry(t, f)
	book := makeBook(1, "No Cover", "Author")
	book.CoverPath = ""

	d := newBookDir(reg, book)

	if _, ok := d.Children()["cover.jpg"]; ok {
		t.Error("bookDir should not contain 'cover.jpg' when CoverPath is empty")
	}
}

func TestBookDirStatReportsTitle(t *testing.T) {
	d := newBookDir(newTestRegistry(t, newTestFS(t)), makeBook(1, "My Title", "Author"))

	s := d.Stat()
	if s.Name != "My Title" {
		t.Errorf("Stat.Name = %q, want %q", s.Name, "My Title")
	}
}

func TestBookDirHasIDChild(t *testing.T) {
	d := newBookDir(newTestRegistry(t, newTestFS(t)), makeBook(1, "Test", "Author"))

	child := d.Children()["id"]
	if child == nil {
		t.Fatal("bookDir should have 'id' child")
	}
	if _, ok := child.(*fs.StaticFile); !ok {
		t.Errorf("'id' child should be a StaticFile")
	}
}

func TestNewEpubExporterPassthrough(t *testing.T) {
	lib := fakeLib{}
	exp := NewEpubExporter(lib)
	book := makeBook(1, "Test", "Author")
	book.EpubFilename = "test.epub"

	if exp.Filename(book) != "test.epub" {
		t.Errorf("Filename = %q, want %q", exp.Filename(book), "test.epub")
	}
}
