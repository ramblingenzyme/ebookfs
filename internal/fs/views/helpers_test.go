package views

import (
	"strconv"
	"testing"
	"time"

	go9pfs "github.com/knusbaum/go9p/fs"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/book"
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
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

// padAt builds a padWidth of exactly n digits, so a table states the width it
// means rather than a maximum that happens to produce it.
func padAt(n int) *padWidth {
	var p padWidth
	p.n.Store(int32(n))
	return &p
}

func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), mock.Editor{})
}

func removeBookFromView(t *testing.T, reg *registry.BookRegistry, view go9pfs.Dir, id int64) *book.BookDir {
	t.Helper()
	bookView, ok := view.(registry.BookView)
	if !ok {
		t.Fatalf("view %T does not implement registry.BookView", view)
	}
	allBooks := NewAllBooksDir(reg)
	for _, child := range allBooks.Children() {
		var dir *book.BookDir
		switch child := child.(type) {
		case *book.BookDir:
			dir = child
		case *namedBookDir:
			dir = child.BookDir
		}
		if dir != nil && dir.Book().ID() == id {
			bookView.Remove(dir)
			return dir
		}
	}
	return nil
}

// A non-zero ttl or maxHandles starts the cleanup goroutine, hence the Close.
func newTestSearchDir(t *testing.T, ttl time.Duration, maxHandles int) (*registry.BookRegistry, *searchDir) {
	t.Helper()
	f := newTestFS(t)
	reg := registry.NewBookRegistry(f, mock.Editor{})
	sd := NewSearchDir(f, reg, ttl, maxHandles)
	t.Cleanup(sd.Close)
	return reg, sd
}

func newTestSearchHandle(t *testing.T) (*registry.BookRegistry, *searchHandleDir) {
	t.Helper()
	reg, sd := newTestSearchDir(t, 0, 0)
	return reg, sd.allocateHandle()
}

func ctlOf(t *testing.T, handle *searchHandleDir) *searchCtlFile {
	t.Helper()
	return fstest.ChildAs[*searchCtlFile](t, handle, "ctl")
}

func hasHandleDir(sd *searchDir, id int64) bool {
	_, ok := sd.Children()[strconv.FormatInt(id, 10)]
	return ok
}
