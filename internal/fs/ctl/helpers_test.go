package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
	"github.com/ramblingenzyme/ebookfs/internal/testutil/libfake"
)

// newTestCtl returns the pieces execute needs, over an empty library.
func newTestCtl(t *testing.T, lib libfake.Lib) (*registry.BookRegistry, *CommandLog) {
	t.Helper()
	return registry.NewBookRegistry(testutil.NewTestFS(t), lib), NewCommandLog(10)
}

// taggedBook builds a minimal book with the given tags, for bulk-edit tests.
func taggedBook(id int64, tags ...string) *library.Book {
	b := testutil.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return testutil.WrapBook(b)
}
