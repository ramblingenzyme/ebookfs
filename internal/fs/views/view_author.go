package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByAuthorDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-author", authorKeys, bookListFactory)
}

// authorKeys is every by-author entry b belongs under, one per credited author.
func authorKeys(b *library.Book) []string {
	authors := b.Authors()
	keys := make([]string, 0, len(authors))
	for _, a := range authors {
		keys = append(keys, a.Name)
	}
	return keys
}
