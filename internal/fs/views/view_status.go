package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByStatusDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-status", statusKeys, bookListFactory)
}

// statusKeys is the one by-status entry b belongs under. Status is drawn from a
// fixed vocabulary, so it needs no name mapping.
func statusKeys(b *library.Book) []string { return []string{b.Status()} }
