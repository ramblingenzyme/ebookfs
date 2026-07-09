// Package testutil provides helpers shared by tests across the module. It is
// imported only from _test.go files, so it is never linked into the binary.
package testutil

import "github.com/ramblingenzyme/ebookfs/library/model"

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
