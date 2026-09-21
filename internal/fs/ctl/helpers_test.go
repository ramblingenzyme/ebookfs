package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
)

// newTestCtl wires a ctl file to search and edit, and hands back the registry
// behind it so a test can file books in. The file owns the command log, which
// a test reads as f.cmdLog.
func newTestCtl(t *testing.T, search SearchDeleter, edit registry.Editor) (*CtlFile, *registry.BookRegistry) {
	t.Helper()
	fsys := util.NewTestFS(t)
	reg := registry.NewBookRegistry(fsys, edit)
	return NewCtlFile(fsys, search, reg, NewCommandLog(10)), reg
}

// taggedBook builds a minimal book with the given tags, for bulk-edit tests.
func taggedBook(id int64, tags ...string) *library.Book {
	b := util.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return util.WrapBook(b)
}

// ctlFor is newTestCtl with no behaviour behind it, for a test that asks only
// which handler a name reaches.
func ctlFor(t *testing.T) *CtlFile {
	t.Helper()
	f, _ := newTestCtl(t, mock.SearchDeleter{}, mock.Editor{})
	return f
}
