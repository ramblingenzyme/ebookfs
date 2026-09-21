package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
)

// The returned registry is for filing books in; the log is f.cmdLog.
func newTestCtl(t *testing.T, search SearchDeleter, edit registry.Editor) (*CtlFile, *registry.BookRegistry) {
	t.Helper()
	fsys := util.NewTestFS(t)
	reg := registry.NewBookRegistry(fsys, edit)
	return NewCtlFile(fsys, search, reg, NewCommandLog(10)), reg
}

func taggedBook(id int64, tags ...string) *library.Book {
	b := util.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return util.WrapBook(b)
}

// ctlFor has no behaviour behind it, for a test asking only which handler a
// name reaches.
func ctlFor(t *testing.T) *CtlFile {
	t.Helper()
	f, _ := newTestCtl(t, mock.SearchDeleter{}, mock.Editor{})
	return f
}
