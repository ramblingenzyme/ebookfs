package views

import (
	"fmt"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
)

// One directory per author set, so co-authors share one, and the last book out
// prunes it.
func TestReaderDir(t *testing.T) {
	reg := newTestRegistry(t)
	d := NewReaderDir(reg, mock.Exporter{StatusList: []string{"unread"}})
	for _, b := range []struct {
		id      int64
		authors []string
	}{
		{1, []string{"Alice"}},
		{2, []string{"Alice"}},
		{3, []string{"Alice", "Bob"}},
	} {
		book := makeBook(b.id, fmt.Sprintf("Book %d", b.id), b.authors...)
		book.EpubPath = fmt.Sprintf("%d.epub", b.id)
		book.Meta.Status = "unread"
		reg.Load(wrapBook(book))
	}

	fstest.ChildCount(t, d, 2)
	fstest.ChildCount(t, fstest.ChildAs[fs.ModDir](t, d, "Alice"), 2)
	fstest.HasChild(t, fstest.ChildAs[fs.ModDir](t, d, "Alice & Bob"), "3.epub")

	removeBookFromView(t, reg, d, 1)
	fstest.ChildCount(t, fstest.ChildAs[fs.ModDir](t, d, "Alice"), 1)

	removeBookFromView(t, reg, d, 2)
	removeBookFromView(t, reg, d, 3)
	fstest.ChildCount(t, d, 0)
}
