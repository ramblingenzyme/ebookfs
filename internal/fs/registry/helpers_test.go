package registry

import (
	"errors"

	"github.com/ramblingenzyme/ebookfs/library"
)

// editor is an Editor. Both hooks error when unset, so a test that reaches the
// edit or read path without stubbing it fails loudly rather than committing
// nothing and passing.
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

var _ Editor = editor{}
