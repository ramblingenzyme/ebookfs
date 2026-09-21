// Package util must not import library. Packages under library have white-box
// tests that import this one, so a library import here is a test-time cycle.
package util

import (
	"errors"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func MakeBook(id int64, title string, authors ...string) *book.ImmutableBook {
	return book.NewImmutableBook(MakeMutableBook(id, title, authors...))
}

func MakeMutableBook(id int64, title string, authors ...string) *book.Book {
	auths := make([]book.Author, len(authors))
	for i, name := range authors {
		auths[i] = book.Author{Name: name}
	}
	return book.NewBook(
		book.Bib{Title: title, Authors: auths},
		book.Meta{ID: id},
		book.Location{},
	)
}

func WrapBook(b *book.Book) *book.ImmutableBook {
	return book.NewImmutableBook(b)
}

func Fixed(b *book.ImmutableBook) func() *book.ImmutableBook {
	return func() *book.ImmutableBook { return b }
}

func NewTestFS(t *testing.T) *fs.FS {
	t.Helper()
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f
}

var ErrTest = errors.New("test error")
