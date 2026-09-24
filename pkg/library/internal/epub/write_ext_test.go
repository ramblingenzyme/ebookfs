package epub_test

import (
	"bytes"
	"os"
	"strings"
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// The Book is given a series because book.Validate refuses an index edit on
// a book without one.
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

// An index-only edit carries no series name, so the name is kept from the OPF.
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

// The index says the book is in a series, but the epub has none. The edit is
// dropped rather than creating an empty series.
func TestWriteBibSeriesIndexOnlyWithoutSeriesInOPF(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))

	book := reindexSeries(t, path, "Phantom Saga", "4")

	if book.Series != nil {
		t.Errorf("series = %+v, want nil — the OPF has none to carry over, and the edit must not invent one", book.Series)
	}
}

func TestWriteBibBlankTitleRejected(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := writeBib(path, bookmodel.Edits{Title: new("   ")}); err == nil {
		t.Fatal("expected error blanking title, got nil")
	}
	book, err := epub.Parse(path)
	if err != nil {
		t.Fatalf("original epub broken after rejected edit: %v", err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title (unchanged)", book.Title)
	}
}

func TestWriteBibTitleChangeClearsStaleSortTitle(t *testing.T) {
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

// library.Edit calls Rewrite even for edits that only touch meta.toml, such as
// a rating. Rewriting the epub then would needlessly update dcterms:modified.
//
// The Bib passed in differs from the file on purpose. If Rewrite read the
// file, the returned title would change.
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

// Setting a sort title on a package with no dc:title adds an empty title,
// which Rewrite rejects. This only happens if the file changed on disk after
// it was indexed.
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

// os.SameFile compares inodes, so it detects a rewrite even when the new zip
// has identical bytes. The returned Bib must still be read from the file.
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

	// Title is paired with its current SortTitle, because a Title on its own
	// clears the sort title and so is a real change.
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

	// A real change must still rewrite the file, or the checks above prove
	// nothing.
	changed := current.Title + " (Revised)"
	if _, err := writeBib(path, bookmodel.Edits{Title: new(changed)}); err != nil {
		t.Fatal(err)
	}
	if os.SameFile(before, statOf()) {
		t.Error("a real title change did not rewrite the epub")
	}
}

// Two authors with the same name would be written to one creator element,
// silently dropping one of them.
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
