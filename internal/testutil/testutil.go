// Package testutil provides helpers shared by tests across the module. It is
// imported only from _test.go files, so it is never linked into the binary.
//
// This package deliberately does NOT import library: library's own internal
// packages (e.g. kepub) have white-box tests that import testutil, so a
// library import here would create a test-time import cycle. Test doubles for
// the library facade interfaces live in the sibling package libfake instead.
package testutil

import (
	"errors"
	"testing"

	"github.com/knusbaum/go9p/fs"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// MakeBook builds a model.Book with the given id, title, and author names,
// leaving every other field at its NewBook default. Author sort names are left
// empty; tests that need them set fields on the returned book directly.
func MakeBook(id int64, title string, authors ...string) *model.Book {
	auths := make([]model.Author, len(authors))
	for i, name := range authors {
		auths[i] = model.Author{Name: name}
	}
	return model.NewBook(
		model.Bib{Title: title, Authors: auths},
		model.Meta{ID: id},
		model.Location{},
	)
}

// Fixed returns a book getter that always yields b, standing in for a live
// snapshot accessor (e.g. bookDir.Book) in tests that construct files directly.
func Fixed(b *model.Book) func() *model.Book {
	return func() *model.Book { return b }
}

// NewTestFS builds an in-memory go9p FS owned by the conventional glenda user,
// with permission checks disabled so tests can drive it directly.
func NewTestFS(t *testing.T) *fs.FS {
	t.Helper()
	f, _ := fs.NewFS("glenda", "glenda", 0555, fs.IgnorePermissions())
	return f
}

// ErrTest is a sentinel error for asserting error propagation through call
// paths, replacing the ad-hoc errors.New("test error") each package used.
var ErrTest = errors.New("test error")
