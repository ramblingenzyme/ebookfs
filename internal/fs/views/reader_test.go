package views

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/fstest"
)

func TestReaderDirAddIncludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "To Read", "Author1")
	b.EpubPath = "To Read.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	fstest.HasChild(t, fstest.ChildAs[fs.ModDir](t, d, "Author1"), "To Read.epub")
}

func TestReaderDirSkipExcludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Finished", "Author2")
	b.Meta.Status = "read"
	reg.Add(wrapBook(b))

	fstest.ChildCount(t, d, 0)
}

func TestReaderDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Only", "Author3")
	b.EpubPath = "Only.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	fstest.ChildCount(t, d, 0)
}

func TestReaderDirCoAuthorSingleDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	// Co-authored books go under a single "Alice & Bob" folder
	fstest.HasChild(t, fstest.ChildAs[fs.ModDir](t, d, "Alice & Bob"), "Joint.epub")
}

func TestReaderDirCoAuthorRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubPath = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))
	reg.Remove(1)

	fstest.NoChild(t, d, "Alice & Bob")
}

func TestReaderDirMultipleBooksSameAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b1 := makeBook(1, "Book A", "SameAuthor")
	b1.EpubPath = "A.epub"
	b1.Meta.Status = "unread"

	b2 := makeBook(2, "Book B", "SameAuthor")
	b2.EpubPath = "B.epub"
	b2.Meta.Status = "unread"

	reg.Add(wrapBook(b1))
	reg.Add(wrapBook(b2))

	fstest.ChildCount(t, fstest.ChildAs[fs.ModDir](t, d, "SameAuthor"), 2)
}

func TestReaderDirWithConvertEnabled(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, readerExporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Convert Me", "AuthorX")
	b.EpubPath = "Convert.epub"
	b.Meta.Status = "unread"
	reg.Add(wrapBook(b))

	fstest.HasChild(t, fstest.ChildAs[fs.ModDir](t, d, "AuthorX"), "Convert.epub")
}
