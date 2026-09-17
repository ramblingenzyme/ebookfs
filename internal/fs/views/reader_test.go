package views

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

func TestReaderDirAddIncludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "To Read", "Author1")
	b.EpubPath = "To Read.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	ad, ok := d.Children()["Author1"]
	if !ok {
		t.Fatal("reader should have 'Author1' subdir")
	}
	ald := ad.(fs.ModDir)
	if _, ok := ald.Children()["To Read.epub"]; !ok {
		t.Errorf("reader should contain 'To Read.epub' under Author1")
	}
}

func TestReaderDirSkipExcludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Finished", "Author2")
	b.Meta.Status = "read"
	reg.Add(wrapBook(b))

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should have no children for 'read' status book, got %d", n)
	}
}

func TestReaderDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Only", "Author3")
	b.EpubPath = "Only.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should be empty after removing only book, got %d children", n)
	}
}

func TestReaderDirCoAuthorSingleDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	// Co-authored books go under a single "Alice & Bob" folder
	ad, ok := d.Children()["Alice & Bob"]
	if !ok {
		t.Fatalf("reader should have 'Alice & Bob' dir, got: %v", dirChildNames(d))
	}
	ald := ad.(fs.ModDir)
	if _, ok := ald.Children()["Joint.epub"]; !ok {
		t.Errorf("reader should contain 'Joint.epub' under 'Alice & Bob'")
	}
}

func TestReaderDirCoAuthorRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	if _, ok := d.Children()["Alice & Bob"]; ok {
		t.Error("'Alice & Bob' dir should be pruned after removal")
	}
}

func TestReaderDirMultipleBooksSameAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b1 := makeBook(1, "Book A", "SameAuthor")
	b1.EpubPath = "A.epub"
	b1.Meta.Status = "unread"

	b2 := makeBook(2, "Book B", "SameAuthor")
	b2.EpubPath = "B.epub"
	b2.Meta.Status = "unread"

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	ad, ok := d.Children()["SameAuthor"]
	if !ok {
		t.Fatal("reader should have 'SameAuthor' subdir with two books")
	}
	ald := ad.(fs.ModDir)
	children := dirChildNames(ald)
	if len(children) != 2 {
		t.Errorf("expected 2 books under SameAuthor, got %d: %v", len(children), children)
	}
}

func TestReaderDirWithConvertEnabled(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Convert Me", "AuthorX")
	b.EpubPath = "Convert.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	ad, ok := d.Children()["AuthorX"]
	if !ok {
		t.Fatal("reader should have 'AuthorX' subdir")
	}
	if _, ok := ad.(fs.ModDir).Children()["Convert.epub"]; !ok {
		t.Error("reader should contain 'Convert.epub'")
	}
}
