package epub_test

import (
	"testing"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

func writeBib(epubPath string, e bookmodel.Edits) (bookmodel.Bib, error) {
	return epub.Rewrite(epubPath, &bookmodel.Book{Location: bookmodel.Location{EpubPath: epubPath}}, e)
}

// An empty Bib won't do: book.Validate refuses an index edit on a book with no
// series, so the Bib is read from the file.
func book(t *testing.T, path string) *bookmodel.Book {
	t.Helper()
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return &bookmodel.Book{Location: bookmodel.Location{EpubPath: path}, Bib: *bib}
}
