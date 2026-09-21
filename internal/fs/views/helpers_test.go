package views

import (
	"strconv"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testing/fstest"
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

func newTestRegistry(t *testing.T) *registry.BookRegistry {
	t.Helper()
	return registry.NewBookRegistry(newTestFS(t), nil)
}

// A non-zero ttl or maxHandles starts the cleanup goroutine, hence the Close.
func newTestSearchDir(t *testing.T, ttl time.Duration, maxHandles int) (*registry.BookRegistry, *searchDir) {
	t.Helper()
	f := newTestFS(t)
	reg := registry.NewBookRegistry(f, nil)
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
