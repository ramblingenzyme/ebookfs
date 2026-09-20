package ctl

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
)

// newTestCtl returns the pieces execute needs. It takes an editor rather than a
// whole library because the registry is the half a command writes through; what
// ctl itself reads is the SearchDeleter passed to execute.
func newTestCtl(t *testing.T, edit registry.Editor) (*registry.BookRegistry, *CommandLog) {
	t.Helper()
	return registry.NewBookRegistry(util.NewTestFS(t), edit), NewCommandLog(10)
}

// taggedBook builds a minimal book with the given tags, for bulk-edit tests.
func taggedBook(id int64, tags ...string) *library.Book {
	b := util.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return util.WrapBook(b)
}
