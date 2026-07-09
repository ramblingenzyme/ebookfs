package fs

import (
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/knusbaum/go9p/proto"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// ---- Registry ----

func TestRegistryAddAndRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := newAllBooksDir(reg)

	b := makeBook(1, "Test Book", "Author")
	reg.Add(b)

	if _, ok := d.Children()["Test Book"]; !ok {
		t.Fatal("books dir should contain 'Test Book' after Add")
	}

	reg.Remove(1)

	if _, ok := d.Children()["Test Book"]; ok {
		t.Error("books dir should not contain 'Test Book' after Remove")
	}
}

func TestRegistryRemoveUnknownID(t *testing.T) {
	reg := newTestRegistry(t)
	newAllBooksDir(reg)

	reg.Remove(999) // should not panic
}

func TestRegistryAddSameIDTwiceUsesSameDir(t *testing.T) {
	reg := newTestRegistry(t)
	allBooks := newAllBooksDir(reg)

	b1 := makeBook(1, "First Title", "Author")
	b2 := makeBook(1, "Second Title", "Author")

	reg.Add(b1)
	reg.Add(b2)

	// dirLocked returns the existing dir and doesn't update the book pointer,
	// so the first title persists. The caller is expected to not reuse IDs.
	children := dirChildNames(allBooks)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d: %v", len(children), children)
	}
	if children[0] != "First Title" {
		t.Errorf("expected 'First Title', got %q", children[0])
	}
}

// ---- BooksDir ----

func TestBooksDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := newAllBooksDir(reg)

	reg.Add(makeBook(1, "Alpha", "Author"))
	reg.Add(makeBook(2, "Beta", "Author"))

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 books, got %d: %v", len(children), children)
	}
}

func TestBooksDirRemoveOnlyOne(t *testing.T) {
	reg := newTestRegistry(t)
	d := newAllBooksDir(reg)

	reg.Add(makeBook(1, "Keep", "Author"))
	reg.Add(makeBook(2, "Remove", "Author"))

	reg.Remove(2)

	if _, ok := d.Children()["Remove"]; ok {
		t.Error("'Remove' should be gone")
	}
	if _, ok := d.Children()["Keep"]; !ok {
		t.Error("'Keep' should remain")
	}
}

func TestBooksDirEmptyNilMap(t *testing.T) {
	reg := newTestRegistry(t)
	d := newAllBooksDir(reg)

	if n := len(d.Children()); n != 0 {
		t.Errorf("new books dir should be empty, got %d children", n)
	}
}

// ---- ByAuthorDir ----

func TestByAuthorDirSingleAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	b := makeBook(1, "My Book", "Alice")
	reg.Add(b)

	ad, ok := d.Children()["Alice"]
	if !ok {
		t.Fatal("by-author should have 'Alice' subdir")
	}
	ald := ad.(*bookListDir)
	if _, ok := ald.Children()["My Book"]; !ok {
		t.Error("Alice's dir should contain 'My Book'")
	}
}

func TestByAuthorDirMultiAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	b := makeBook(1, "Joint Work", "Alice", "Bob")
	reg.Add(b)

	for _, name := range []string{"Alice", "Bob"} {
		ad, ok := d.Children()[name]
		if !ok {
			t.Fatalf("by-author should have %q subdir", name)
		}
		ald := ad.(*bookListDir)
		if _, ok := ald.Children()["Joint Work"]; !ok {
			t.Errorf("%s's dir should contain 'Joint Work'", name)
		}
	}
}

func TestByAuthorDirRemoveLastBookPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	b := makeBook(1, "Only Book", "Charlie")
	reg.Add(b)

	reg.Remove(1)

	if _, ok := d.Children()["Charlie"]; ok {
		t.Error("Charlie's dir should be pruned after last book removed")
	}
}

func TestByAuthorDirMultipleBooksSameAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	reg.Add(makeBook(1, "Book A", "Dana"))
	reg.Add(makeBook(2, "Book B", "Dana"))

	ad, ok := d.Children()["Dana"]
	if !ok {
		t.Fatal("by-author should have 'Dana' subdir")
	}
	ald := ad.(*bookListDir)
	children := dirChildNames(ald)
	if len(children) != 2 {
		t.Errorf("expected 2 books under Dana, got %d: %v", len(children), children)
	}
}

func TestByAuthorDirRemoveOneOfTwo(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	reg.Add(makeBook(1, "Keep", "Dana"))
	reg.Add(makeBook(2, "Remove", "Dana"))

	reg.Remove(2)

	ad := d.Children()["Dana"].(*bookListDir)
	if _, ok := ad.Children()["Remove"]; ok {
		t.Error("'Remove' should be gone from Dana's dir")
	}
	if _, ok := ad.Children()["Keep"]; !ok {
		t.Error("'Keep' should remain in Dana's dir")
	}
}

func TestByAuthorDirBookNoAuthors(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByAuthorDir(reg)

	b := makeBook(1, "No Author")
	b.Authors = nil // force nil authors
	reg.Add(b)

	if n := len(d.Children()); n != 0 {
		t.Errorf("expected 0 children for authorless book, got %d", n)
	}
}

func TestByAuthorDirRemoveAuthorlessBook(t *testing.T) {
	reg := newTestRegistry(t)
	newByAuthorDir(reg)

	b := makeBook(1, "No Author")
	b.Authors = nil
	reg.Add(b)
	reg.Remove(1) // should not panic
}

// ---- BySeriesDir ----

func TestBySeriesDirAddWithSeries(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b := makeBook(1, "The Hobbit", "Tolkien")
	b.Series = &model.SeriesRef{Name: "Middle-earth", Index: 1.0}

	reg.Add(b)

	sd, ok := d.Children()["Middle-earth"]
	if !ok {
		t.Fatal("by-series should have 'Middle-earth' subdir")
	}
	sld := sd.(*seriesBookListDir)
	children := dirChildNames(sld)
	if len(children) != 1 {
		t.Fatalf("expected 1 book in series, got %d: %v", len(children), children)
	}
}

func TestBySeriesDirNoSeries(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b := makeBook(1, "Standalone", "Author")
	reg.Add(b)

	if n := len(d.Children()); n != 0 {
		t.Errorf("by-series should have no children for series-less book, got %d", n)
	}
}

func TestBySeriesDirRemoveLastBookPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b := makeBook(1, "Only in Series", "Author")
	b.Series = &model.SeriesRef{Name: "S", Index: 1.0}
	reg.Add(b)

	reg.Remove(1)

	if _, ok := d.Children()["S"]; ok {
		t.Error("series dir should be pruned after last book removed")
	}
}

func TestBySeriesDirMultipleBooksSameSeries(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b1 := makeBook(1, "Book One", "Author")
	b1.Series = &model.SeriesRef{Name: "Saga", Index: 1.0}
	b2 := makeBook(2, "Book Two", "Author")
	b2.Series = &model.SeriesRef{Name: "Saga", Index: 2.0}

	reg.Add(b1)
	reg.Add(b2)

	sd := d.Children()["Saga"].(*seriesBookListDir)
	children := dirChildNames(sd)
	if len(children) != 2 {
		t.Fatalf("expected 2 books in Saga, got %d: %v", len(children), children)
	}
}

func TestBySeriesDirRemoveOneOfTwo(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b1 := makeBook(1, "Keep", "Author")
	b1.Series = &model.SeriesRef{Name: "S", Index: 1.0}
	b2 := makeBook(2, "Remove", "Author")
	b2.Series = &model.SeriesRef{Name: "S", Index: 2.0}

	reg.Add(b1)
	reg.Add(b2)
	reg.Remove(2)

	sd := d.Children()["S"].(*seriesBookListDir)
	if _, ok := sd.Children()["Remove"]; ok {
		t.Error("'Remove' entry should be gone")
	}
}

func TestBySeriesDirNilSeriesNotAdded(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b1 := makeBook(1, "Has Series", "Author")
	b1.Series = &model.SeriesRef{Name: "S", Index: 1.0}
	b2 := makeBook(2, "No Series", "Author")

	reg.Add(b1)
	reg.Add(b2)

	if n := len(d.Children()); n != 1 {
		t.Errorf("expected 1 series child, got %d", n)
	}
}

func TestBySeriesDirReaddAfterRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	b := makeBook(1, "Back and Forth", "Author")
	b.Series = &model.SeriesRef{Name: "S", Index: 1.0}
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["S"]; ok {
		t.Error("series dir should be gone after remove")
	}

	reg.Add(b)
	if _, ok := d.Children()["S"]; !ok {
		t.Error("series dir should be back after re-add")
	}
}

// ---- ByIDDir ----

func TestByIDDirAdd(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(b)

	if _, ok := d.Children()["1. Test"]; !ok {
		t.Errorf("by-id should contain '1. Test', got: %v", dirChildNames(d))
	}
}

func TestByIDDirRemove(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByIDDir(reg)

	b := makeBook(1, "Test", "Author")
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["1. Test"]; ok {
		t.Error("by-id should not contain entry after remove")
	}
}

func TestByIDDirMultipleBooks(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByIDDir(reg)

	reg.Add(makeBook(1, "Alpha", "Author"))
	reg.Add(makeBook(2, "Beta", "Author"))

	children := dirChildNames(d)
	if len(children) != 2 {
		t.Fatalf("expected 2 entries, got %d: %v", len(children), children)
	}
}

func TestByIDDirRemoveUnknown(t *testing.T) {
	reg := newTestRegistry(t)
	newByIDDir(reg)

	reg.Remove(999) // should not panic
}

func TestByIDDirTitleChangeReflected(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByIDDir(reg)

	b := makeBook(1, "Original", "Author")
	reg.Add(b)

	if _, ok := d.Children()["1. Original"]; !ok {
		t.Fatal("by-id should contain '1. Original'")
	}

	// Remove and re-add with different title (simulating an edit)
	reg.Remove(1)
	b2 := makeBook(1, "Updated", "Author")
	reg.Add(b2)

	if _, ok := d.Children()["1. Updated"]; !ok {
		t.Error("by-id should contain '1. Updated' after re-add")
	}
	if _, ok := d.Children()["1. Original"]; ok {
		t.Error("by-id should not contain '1. Original' after update")
	}
}

// ---- ReaderDir ----

func TestReaderDirAddIncludedStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "To Read", "Author1")
	b.EpubFilename = "To Read.epub"
	b.Meta.Status = "unread"
	reg.Add(b)

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
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Finished", "Author2")
	b.Meta.Status = "read"
	reg.Add(b)

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should have no children for 'read' status book, got %d", n)
	}
}

func TestReaderDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Only", "Author3")
	b.EpubFilename = "Only.epub"
	b.Meta.Status = "unread"
	reg.Add(b)
	reg.Remove(1)

	if n := len(d.Children()); n != 0 {
		t.Errorf("reader should be empty after removing only book, got %d children", n)
	}
}

func TestReaderDirCoAuthorSingleDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubFilename = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(b)

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
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Joint", "Alice", "Bob")
	b.EpubFilename = "Joint.epub"
	b.Meta.Status = "unread"
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["Alice & Bob"]; ok {
		t.Error("'Alice & Bob' dir should be pruned after removal")
	}
}

func TestReaderDirMultipleBooksSameAuthor(t *testing.T) {
	reg := newTestRegistry(t)
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b1 := makeBook(1, "Book A", "SameAuthor")
	b1.EpubFilename = "A.epub"
	b1.Meta.Status = "unread"

	b2 := makeBook(2, "Book B", "SameAuthor")
	b2.EpubFilename = "B.epub"
	b2.Meta.Status = "unread"

	reg.Add(b1)
	reg.Add(b2)

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
	d := newReaderDir(reg, libfake.Exporter{StatusList: []string{"unread"}})

	b := makeBook(1, "Convert Me", "AuthorX")
	b.EpubFilename = "Convert.epub"
	b.Meta.Status = "unread"
	reg.Add(b)

	ad, ok := d.Children()["AuthorX"]
	if !ok {
		t.Fatal("reader should have 'AuthorX' subdir")
	}
	if _, ok := ad.(fs.ModDir).Children()["Convert.epub"]; !ok {
		t.Error("reader should contain 'Convert.epub'")
	}
}

// ---- PruneEmpty ----

func TestPruneEmptyNoOpForMissingChild(t *testing.T) {
	g := newGroupingDir(newTestFS(t), "test")
	g.pruneEmpty("nonexistent")
	// Should not panic.
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

func TestBySeriesDirRemoveNilSeriesNoOp(t *testing.T) {
	reg := newTestRegistry(t)
	d := newBySeriesDir(reg)

	book := makeBook(1, "No Series", "Author")
	bd := newBookDir(reg, book)

	d.remove(bd) // Should not panic — early return when Series is nil
}

// ---- ByTagDir ----

func TestByTagDirAddBookWithTags(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Tagged Book", "Author")
	b.Meta.Tags = []string{"fiction", "fantasy"}
	reg.Add(b)

	for _, tag := range []string{"fiction", "fantasy"} {
		td, ok := d.Children()[tag]
		if !ok {
			t.Fatalf("by-tag should have %q subdir", tag)
		}
		bld := td.(*bookListDir)
		if _, ok := bld.Children()["Tagged Book"]; !ok {
			t.Errorf("%s's dir should contain 'Tagged Book'", tag)
		}
	}
}

func TestByTagDirNoTags(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Untagged", "Author")
	b.Meta.Tags = nil
	reg.Add(b)

	if n := len(d.Children()); n != 0 {
		t.Errorf("by-tag should have no children for tagless book, got %d", n)
	}
}

func TestByTagDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Only Tagged", "Author")
	b.Meta.Tags = []string{"ephemeral"}
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["ephemeral"]; ok {
		t.Error("tag subdir should be pruned after last book removed")
	}
}

func TestByTagDirMultipleBooksSameTag(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b1 := makeBook(1, "Book A", "Author")
	b1.Meta.Tags = []string{"scifi"}
	b2 := makeBook(2, "Book B", "Author")
	b2.Meta.Tags = []string{"scifi"}

	reg.Add(b1)
	reg.Add(b2)

	td := d.Children()["scifi"].(*bookListDir)
	children := dirChildNames(td)
	if len(children) != 2 {
		t.Fatalf("expected 2 books under scifi, got %d: %v", len(children), children)
	}
}

func TestByTagDirRemoveOneOfTwo(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b1 := makeBook(1, "Keep", "Author")
	b1.Meta.Tags = []string{"tag"}
	b2 := makeBook(2, "Remove", "Author")
	b2.Meta.Tags = []string{"tag"}

	reg.Add(b1)
	reg.Add(b2)
	reg.Remove(2)

	td := d.Children()["tag"].(*bookListDir)
	if _, ok := td.Children()["Remove"]; ok {
		t.Error("'Remove' book should be gone from tag dir")
	}
	if _, ok := td.Children()["Keep"]; !ok {
		t.Error("'Keep' book should remain in tag dir")
	}
}

func TestByTagDirTagWithSlash(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"a/b"}
	reg.Add(b)

	if _, ok := d.Children()["a_b"]; !ok {
		t.Fatalf("by-tag should have 'a_b' subdir for tag 'a/b', got: %v", dirChildNames(d))
	}
	if _, ok := d.Children()["a/b"]; ok {
		t.Error("by-tag should NOT have 'a/b' subdir (slash not valid in 9P names)")
	}
}

func TestByTagDirRemoveWithSlashTag(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Slash Tag", "Author")
	b.Meta.Tags = []string{"x/y"}
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["x_y"]; ok {
		t.Error("tag subdir should be pruned after remove")
	}
}

func TestByTagDirEditTags(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByTagDir(reg)

	b := makeBook(1, "Retagged", "Author")
	b.Meta.Tags = []string{"oldtag"}
	reg.Add(b)

	if _, ok := d.Children()["oldtag"]; !ok {
		t.Fatal("by-tag should have 'oldtag' subdir")
	}

	reg.Remove(1)
	b.Meta.Tags = []string{"newtag"}
	reg.Add(b)

	if _, ok := d.Children()["oldtag"]; ok {
		t.Error("'oldtag' subdir should be pruned after retag")
	}
	td, ok := d.Children()["newtag"]
	if !ok {
		t.Fatal("by-tag should have 'newtag' subdir after retag")
	}
	if _, ok := td.(*bookListDir).Children()["Retagged"]; !ok {
		t.Error("'Retagged' should appear under 'newtag'")
	}
}

// ---- ByStatusDir ----

func TestByStatusDirAddBook(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	b := makeBook(1, "My Book", "Author")
	reg.Add(b)

	sd, ok := d.Children()["unread"]
	if !ok {
		t.Fatal("by-status should have 'unread' subdir")
	}
	bld := sd.(*bookListDir)
	if _, ok := bld.Children()["My Book"]; !ok {
		t.Error("unread dir should contain 'My Book'")
	}
}

func TestByStatusDirDifferentStatuses(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	b1 := makeBook(1, "Unread Book", "Author")
	b1.Meta.Status = "unread"
	b2 := makeBook(2, "Read Book", "Author")
	b2.Meta.Status = "read"

	reg.Add(b1)
	reg.Add(b2)

	for _, status := range []string{"unread", "read"} {
		sd, ok := d.Children()[status]
		if !ok {
			t.Fatalf("by-status should have %q subdir", status)
		}
		bld := sd.(*bookListDir)
		if n := len(bld.Children()); n != 1 {
			t.Errorf("expected 1 book in %q, got %d", status, n)
		}
	}
}

func TestByStatusDirRemoveLastPrunesDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	b := makeBook(1, "Only", "Author")
	b.Meta.Status = "reading"
	reg.Add(b)
	reg.Remove(1)

	if _, ok := d.Children()["reading"]; ok {
		t.Error("status subdir should be pruned after last book removed")
	}
}

func TestByStatusDirMultipleBooksSameStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	reg.Add(makeBook(1, "Book A", "Author"))
	reg.Add(makeBook(2, "Book B", "Author"))

	sd := d.Children()["unread"].(*bookListDir)
	children := dirChildNames(sd)
	if len(children) != 2 {
		t.Fatalf("expected 2 books under unread, got %d: %v", len(children), children)
	}
}

func TestByStatusDirRemoveOneOfTwo(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	b1 := makeBook(1, "Keep", "Author")
	b2 := makeBook(2, "Remove", "Author")

	reg.Add(b1)
	reg.Add(b2)
	reg.Remove(2)

	sd := d.Children()["unread"].(*bookListDir)
	if _, ok := sd.Children()["Remove"]; ok {
		t.Error("'Remove' book should be gone from status dir")
	}
	if _, ok := sd.Children()["Keep"]; !ok {
		t.Error("'Keep' book should remain in status dir")
	}
}

func TestByStatusDirEditStatus(t *testing.T) {
	reg := newTestRegistry(t)
	d := newByStatusDir(reg)

	b := makeBook(1, "Status Change", "Author")
	b.Meta.Status = "unread"
	reg.Add(b)

	if _, ok := d.Children()["unread"]; !ok {
		t.Fatal("by-status should have 'unread' subdir")
	}

	reg.Remove(1)
	b.Meta.Status = "read"
	reg.Add(b)

	if _, ok := d.Children()["unread"]; ok {
		t.Error("'unread' subdir should be pruned after status change")
	}
	sd, ok := d.Children()["read"]
	if !ok {
		t.Fatal("by-status should have 'read' subdir after status change")
	}
	if _, ok := sd.(*bookListDir).Children()["Status Change"]; !ok {
		t.Error("'Status Change' should appear under 'read'")
	}
}

func TestBooksDirDuplicateTitles(t *testing.T) {
	reg := newTestRegistry(t)
	d := newAllBooksDir(reg)

	b1 := makeBook(1, "Same Title", "Alice")
	b2 := makeBook(2, "Same Title", "Bob")

	reg.Add(b1)
	if _, ok := d.Children()["Same Title"]; !ok {
		t.Fatal("first book should appear under plain title")
	}

	reg.Add(b2)
	if _, ok := d.Children()["Same Title"]; !ok {
		t.Error("first book should remain at plain title")
	}
	if _, ok := d.Children()["Same Title (2)"]; !ok {
		t.Error("second book should appear as 'Same Title (2)'")
	}
	if len(d.Children()) != 2 {
		t.Errorf("expected 2 children, got %d", len(d.Children()))
	}

	// Removing the first book should not affect the second.
	reg.Remove(1)
	if _, ok := d.Children()["Same Title"]; ok {
		t.Error("first book should be removed")
	}
	if _, ok := d.Children()["Same Title (2)"]; !ok {
		t.Error("second book should remain after first is removed")
	}
	if len(d.Children()) != 1 {
		t.Errorf("expected 1 child after removing first book, got %d", len(d.Children()))
	}

	// Removing the second book cleans up the disambiguated entry.
	reg.Remove(2)
	if _, ok := d.Children()["Same Title (2)"]; ok {
		t.Error("second book should be removed")
	}
	if len(d.Children()) != 0 {
		t.Errorf("expected 0 children after removing both, got %d", len(d.Children()))
	}
}
