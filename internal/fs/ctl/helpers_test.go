package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func newTestCtl(t *testing.T, search mock.SearchDeleter, edit mock.Editor) *CtlFile {
	t.Helper()
	fsys := util.NewTestFS(t)
	lib := mock.Library{Editor: edit, SearchDeleter: search}
	return NewCtlFile(fsys, lib, NewCommandLog(10))
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
	return newTestCtl(t, mock.SearchDeleter{}, mock.Editor{})
}
