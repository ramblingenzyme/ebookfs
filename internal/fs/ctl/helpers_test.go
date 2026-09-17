package ctl

import (
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/internal/testutil"
)

// newTestCtl returns the pieces execute needs. It takes an editor rather than a
// whole library because the registry is the half a command writes through; what
// ctl itself reads is the searchDeleter passed to execute.
func newTestCtl(t *testing.T, edit editor) (*registry.BookRegistry, *CommandLog) {
	t.Helper()
	return registry.NewBookRegistry(testutil.NewTestFS(t), edit), NewCommandLog(10)
}

// taggedBook builds a minimal book with the given tags, for bulk-edit tests.
func taggedBook(id int64, tags ...string) *library.Book {
	b := testutil.MakeMutableBook(id, "Title", "Author")
	b.Meta.Tags = tags
	return testutil.WrapBook(b)
}

// searchDeleter is a SearchDeleter, the half of the backend ctl reads and
// deletes through. No hook means an empty library and a delete that succeeds,
// which is what the dispatch tests assert against.
type searchDeleter struct {
	SearchFn func(library.Query) ([]*library.Book, error)
	DeleteFn func(int64) error
}

func (l searchDeleter) Search(q library.Query) ([]*library.Book, error) {
	if l.SearchFn != nil {
		return l.SearchFn(q)
	}
	return nil, nil
}

func (l searchDeleter) Delete(id int64) error {
	if l.DeleteFn != nil {
		return l.DeleteFn(id)
	}
	return nil
}

var _ SearchDeleter = searchDeleter{}

// editor is a registry.Editor, the half an edit lands through. Both hooks error
// when unset so a command that reaches the edit path without stubbing it fails
// loudly rather than reporting success.
type editor struct {
	EditFn    func(int64, library.Edits) (*library.Book, error)
	ContentFn func(int64) (library.EpubReader, error)
}

func (e editor) Edit(id int64, edits library.Edits) (*library.Book, error) {
	if e.EditFn != nil {
		return e.EditFn(id, edits)
	}
	return nil, errors.New("editor: no EditFn")
}

func (e editor) Content(id int64) (library.EpubReader, error) {
	if e.ContentFn != nil {
		return e.ContentFn(id)
	}
	return nil, errors.New("editor: no ContentFn")
}

var _ registry.Editor = editor{}
