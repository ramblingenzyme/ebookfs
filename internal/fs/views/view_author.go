package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByAuthorDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-author", authorKeys, bookListFactory)
}

func authorKeys(b *library.Book) []string {
	authors := b.Authors()
	names := make([]string, len(authors))
	for i, a := range authors {
		names[i] = a.Name
	}
	return nameableKeys(names)
}
