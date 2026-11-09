package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByTagDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-tag", tagKeys, bookListFactory)
}

func tagKeys(b *library.Book) []string { return nameableKeys(b.Tags()) }
