package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByTagDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-tag", tagKeys, bookListFactory)
}

// tagKeys is every by-tag entry b belongs under. An empty tag names no
// directory, so it is skipped rather than filed under the placeholder
// naming.PathSafe would give it.
func tagKeys(b *library.Book) []string {
	tags := b.Tags()
	keys := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		keys = append(keys, tag)
	}
	return keys
}
