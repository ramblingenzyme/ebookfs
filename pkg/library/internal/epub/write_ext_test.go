// What Rewrite does with an bookmodel.Edits that the epub package's own fields cannot
// say: a nil means the edit did not name the field, so one half of a series
// carries the other over, a retitled book drops its stale sort title, and an
// edit naming nothing at all is not an edit.
//
// Also the two refusals that are this layer's: bookmodel.Validate as a backstop, and the
// title check that must run before anything is written.

package epub_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// reindexSeries applies an index-only series edit to the epub at path, with the
// book model claiming series. bookmodel.Validate refuses a SeriesIndex edit on a book
// with no series at all, so the model has to carry one, the shape library.Edit
// hands in after reading the book from the index.
func reindexSeries(t *testing.T, path, series, index string) bookmodel.Bib {
	t.Helper()
	b := &bookmodel.Book{
		Location: bookmodel.Location{EpubPath: path},
		Bib:      bookmodel.Bib{Series: &bookmodel.SeriesRef{Name: series, Index: "1"}},
	}
	bib, err := epub.Rewrite(path, b, bookmodel.Edits{SeriesIndex: new(index)})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	return bib
}

// An index-only edit has no name to write, so the OPF is the only source. Get
// it wrong and moving a book within its series silently drops the series.
func TestWriteBibSeriesIndexOnlyKeepsName(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{{"epub3", epubtest.OPF3}, {"epub2", epubtest.OPF2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(tc.opf))
			if _, err := writeBib(path, bookmodel.Edits{Series: new("The Saga"), SeriesIndex: new("1")}); err != nil {
				t.Fatal(err)
			}

			book := reindexSeries(t, path, "The Saga", "4")

			if book.Series == nil || book.Series.Name != "The Saga" {
				t.Errorf("series = %v after an index-only edit, want it carried over from the OPF", book.Series)
			}
			if book.Series == nil || book.Series.Index != "4" {
				t.Errorf("series index = %v, want 4", book.Series.Index)
			}
		})
	}
}

// Index says the book is in a series, the epub has no such metadata. With no
// name to write against, the edit is dropped rather than inventing an empty
// collection.
func TestWriteBibSeriesIndexOnlyWithoutSeriesInOPF(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3)) // no series metadata

	book := reindexSeries(t, path, "Phantom Saga", "4")

	if book.Series != nil {
		t.Errorf("series = %+v, want nil — the OPF has none to carry over, and the edit must not invent one", book.Series)
	}
	// SeriesIndex is deliberately not asserted: translateSeries defaults it to
	// 1 for every book with a series, and only sets Series when the name is
	// non-empty, so the position never escapes.
}

func TestWriteBibBlankTitleRejected(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := writeBib(path, bookmodel.Edits{Title: new("   ")}); err == nil {
		t.Fatal("expected error blanking title, got nil")
	}
	// Original must be untouched and still valid.
	book, err := epub.Parse(path)
	if err != nil {
		t.Fatalf("original epub broken after rejected edit: %v", err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title (unchanged)", book.Title)
	}
}

func TestWriteBibTitleChangeClearsStaleSortTitle(t *testing.T) {
	// epubtest.OPF3 starts with sort title "Title, Original"; changing the title without a
	// new sort title must clear it rather than leave a value derived from the old
	// title.
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	before, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.SortTitle != "Title, Original" {
		t.Fatalf("precondition: sort title = %q, want Title, Original", before.SortTitle)
	}

	book, err := writeBib(path, bookmodel.Edits{Title: new("Wuthering Heights")})
	if err != nil {
		t.Fatal(err)
	}
	if book.SortTitle != "" {
		t.Errorf("sort title = %q, want empty after a title change", book.SortTitle)
	}
}

// Rewrite's short circuit, as its doc promises: an bookmodel.Edits carrying nothing
// returns b.Bib verbatim and leaves the file alone. library.Edit depends on it:
// it calls Rewrite on every edit including meta-only ones, so a rewrite here
// would rebuild the zip and restamp dcterms:modified for a change to a rating.
//
// The Bib handed in deliberately disagrees with the file, so a Rewrite that
// re-parsed instead of short-circuiting would return the file's title rather
// than this one.
func TestRewriteWithNoEditsIsATotalNoOp(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(`<dc:title>On Disk</dc:title><dc:creator>Alice</dc:creator>`))

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read epub: %v", err)
	}

	b := book(t, path)
	b.Bib.Title = "Not What The File Says"

	got, err := epub.Rewrite(path, b, bookmodel.Edits{})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}
	if got.Title != "Not What The File Says" {
		t.Errorf("Title = %q, want the caller's Bib returned untouched", got.Title)
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("re-read epub: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the epub was rewritten for an edit that asked for nothing")
	}
}

// rewriteEpub re-parses before renaming, so a write producing an unreadable book
// is abandoned. The trigger is a sort-title-only edit on a package with no
// dc:title: the refinement mints an empty element, and an empty title is not a
// book. Reachable only if the file changed on disk after being indexed.
func TestFailedValidationLeavesTheOriginal(t *testing.T) {
	opf := epubtest.OPF3
	for _, drop := range []string{
		`    <dc:title id="t1">Original Title</dc:title>` + "\n",
		`    <meta refines="#t1" property="file-as">Title, Original</meta>` + "\n",
	} {
		if !strings.Contains(string(opf), drop) {
			t.Fatalf("epubtest.OPF3 changed; cannot drop %q to build a titleless package", drop)
		}
		opf = epubtest.PackageDoc(strings.Replace(string(opf), drop, "", 1))
	}
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opf))

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := writeBib(path, bookmodel.Edits{SortTitle: new("Hobbit, The")}); err == nil {
		t.Fatal("expected the rewrite to be rejected, got nil")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Error("a rejected rewrite modified the original epub")
	}
}

// os.SameFile compares device and inode, so it catches the rewrite even when the
// rebuilt zip is byte-identical. The returned Bib still comes from the file,
// the part the skip must not cost.
func TestNoOpBibEditDoesNotRewriteTheFile(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	statOf := func() os.FileInfo {
		t.Helper()
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return fi
	}

	before := statOf()
	current, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}

	// A Title without a new SortTitle clears the stale sort title, which is a
	// real change, and epubtest.OPF3 carries one.
	for _, tc := range []struct {
		name string
		e    bookmodel.Edits
	}{
		{"title with its existing sort title", bookmodel.Edits{Title: &current.Title, SortTitle: &current.SortTitle}},
		{"description already equal", bookmodel.Edits{Description: &current.Description}},
		{"language already equal", bookmodel.Edits{Language: &current.Language}},
	} {
		bib, err := writeBib(path, tc.e)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !os.SameFile(before, statOf()) {
			t.Errorf("%s: an edit asking for what the OPF already carries rewrote the epub", tc.name)
		}
		if bib.Title != current.Title {
			t.Errorf("%s: bib.Title = %q, want %q read back from the file", tc.name, bib.Title, current.Title)
		}
	}

	// Control: a real change must still land, or the check above proves nothing.
	changed := current.Title + " (Revised)"
	if _, err := writeBib(path, bookmodel.Edits{Title: new(changed)}); err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, statOf()) {
		t.Error("a real title change did not rewrite the epub")
	}
}

// Two creators of one name have no meaning in either spec, and reusing one
// element for both would silently collapse the list. Validate rejects it
// and Rewrite re-checks, so an unvalidated Edits cannot reach the file.
func TestRewriteRefusesDuplicateAuthors(t *testing.T) {
	path := epubtest.Build(t, epubtest.RichOPF3)
	before := epubtest.ReadEntry(t, path, epubtest.OPFPath)

	authors := []bookmodel.Author{{Name: "Jane Doe"}, {Name: "Jane Doe"}}
	if _, err := writeBib(path, bookmodel.Edits{Authors: &authors}); err == nil {
		t.Fatal("a duplicated author was accepted")
	}
	if !bytes.Equal(before, epubtest.ReadEntry(t, path, epubtest.OPFPath)) {
		t.Error("the epub was rewritten by a refused edit")
	}
}
