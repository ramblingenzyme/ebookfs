// The two calls that put an edit through, shared by the parse and write tests.
// The corpus itself is internal/epubtest, shared with the epub package's suite.

package epub_test

import (
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
)

// writeBib applies edits to the package document of the epub at epubPath,
// rewrites the file in place, and returns the re-parsed Bib. Production code
// drives that flow through library.Edit.
func writeBib(epubPath string, e bookmodel.Edits) (bookmodel.Bib, error) {
	return epub.Rewrite(epubPath, &bookmodel.Book{Location: bookmodel.Location{EpubPath: epubPath}}, e)
}

// book builds the Book that Rewrite validates against, the way library.Edit
// does: from the file's current state. An index edit is refused unless the book
// already has a series, so an empty Bib is not a usable stand-in.
func book(t *testing.T, path string) *bookmodel.Book {
	t.Helper()
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return &bookmodel.Book{Location: bookmodel.Location{EpubPath: path}, Bib: *bib}
}
