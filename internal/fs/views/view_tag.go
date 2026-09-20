package views

import (
	"strings"

	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewByTagDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-tag", tagKeys, bookListFactory)
}

// tagEntryName maps a tag to its by-tag directory name. A '/' would otherwise
// make an entry a 9P client cannot walk to.
func tagEntryName(tag string) string {
	return strings.ReplaceAll(tag, "/", "_")
}

// tagKeys is every by-tag entry b belongs under. An empty tag names no
// directory, so it is skipped rather than filed under "".
func tagKeys(b *library.Book) []string {
	tags := b.Tags()
	keys := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == "" {
			continue
		}
		keys = append(keys, tagEntryName(tag))
	}
	return keys
}
