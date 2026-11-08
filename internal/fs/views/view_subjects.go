package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewBySubjectsDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-subjects", subjectKeys, bookListFactory)
}

// subjectKeys is every by-subjects entry b belongs under. An empty subject
// names no directory, so it is skipped rather than filed under the placeholder
// naming.PathSafe would give it.
func subjectKeys(b *library.Book) []string {
	subjects := b.Subjects()
	keys := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		if subject == "" {
			continue
		}
		keys = append(keys, subject)
	}
	return keys
}
