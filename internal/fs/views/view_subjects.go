package views

import (
	"github.com/ramblingenzyme/ebookfs/internal/fs/registry"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

func NewBySubjectsDir(reg *registry.BookRegistry) *keyedDir {
	return newKeyedDir(reg, "by-subjects", subjectKeys, bookListFactory)
}

func subjectKeys(b *library.Book) []string { return nameableKeys(b.Subjects()) }
