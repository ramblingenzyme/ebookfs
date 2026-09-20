package views

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
)

var (
	makeBook  = util.MakeMutableBook
	newTestFS = util.NewTestFS
	wrapBook  = util.WrapBook
)

func makeBookWithSeries(id int64, title, author string, seriesName, seriesIndex string) *library.Book {
	b := makeBook(id, title, author)
	b.Series = &library.Series{Name: seriesName, Index: seriesIndex}
	return wrapBook(b)
}

// newTestRegistry builds a registry over a fresh in-memory FS with no backing
// library, for driving views through their Add/Remove notifications.
func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), nil)
}
