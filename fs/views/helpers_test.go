package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/library/model"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

var (
	makeBook  = testutil.MakeMutableBook
	newTestFS = testutil.NewTestFS
)

func wrapBook(b *book.Book) *library.Book {
	return book.NewImmutableBook(b)
}

func makeBookWithSeries(id int64, title, author string, seriesName, seriesIndex string) *library.Book {
	b := makeBook(id, title, author)
	b.Series = &model.SeriesRef{Name: seriesName, Index: seriesIndex}
	return wrapBook(b)
}

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library, for driving views through their Add/Remove notifications.
func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), nil)
}

func dirChildNames(d fs.Dir) []string {
	var names []string
	for name := range d.Children() {
		names = append(names, name)
	}
	return names
}
