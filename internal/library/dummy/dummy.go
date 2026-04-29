package dummy

import (
	"context"
	"io"

	"github.com/ramblingenzyme/ebookfs/library"
	"github.com/ramblingenzyme/ebookfs/query"
)

type book struct {
	library.Book
	authors []string
	tags    []string
	series  string
}

type Library struct {
	books []book
}

func New() *Library {
	return &Library{
		books: []book{
			{
				Book:    library.Book{ID: 1, Title: "Crime and Punishment", EpubPath: "/dev/null", CoverPath: "/dev/null", MetadataPath: "/dev/null"},
				authors: []string{"Fyodor Dostoevsky"},
				tags:    []string{"classic", "novel", "russian"},
			},
			{
				Book:    library.Book{ID: 2, Title: "The Left Hand of Darkness", EpubPath: "/dev/null", CoverPath: "/dev/null", MetadataPath: "/dev/null"},
				authors: []string{"Ursula K. Le Guin"},
				tags:    []string{"sci-fi", "novel"},
				series:  "Hainish Cycle",
			},
			{
				Book:    library.Book{ID: 3, Title: "The Dispossessed", EpubPath: "/dev/null", CoverPath: "/dev/null", MetadataPath: "/dev/null"},
				authors: []string{"Ursula K. Le Guin"},
				tags:    []string{"sci-fi", "novel"},
				series:  "Hainish Cycle",
			},
			{
				Book:    library.Book{ID: 4, Title: "Foundation", EpubPath: "/dev/null", CoverPath: "/dev/null", MetadataPath: "/dev/null"},
				authors: []string{"Isaac Asimov"},
				tags:    []string{"sci-fi", "novel"},
				series:  "Foundation",
			},
			{
				Book:    library.Book{ID: 5, Title: "The Brothers Karamazov", EpubPath: "/dev/null", CoverPath: "/dev/null", MetadataPath: "/dev/null"},
				authors: []string{"Fyodor Dostoevsky"},
				tags:    []string{"classic", "novel", "russian"},
			},
		},
	}
}

func (l *Library) Books(ctx context.Context, q query.Query) ([]library.Book, error) {
	var result []library.Book
	for _, b := range l.books {
		if matches(b, q) {
			result = append(result, b.Book)
		}
	}
	return result, nil
}

func (l *Library) Values(ctx context.Context, predicateType string, q query.Query) ([]string, error) {
	seen := make(map[string]bool)
	var result []string
	for _, b := range l.books {
		if !matches(b, q) {
			continue
		}
		var vals []string
		switch predicateType {
		case "author":
			vals = b.authors
		case "tag":
			vals = b.tags
		case "series":
			if b.series != "" {
				vals = []string{b.series}
			}
		}
		for _, v := range vals {
			if !seen[v] {
				seen[v] = true
				result = append(result, v)
			}
		}
	}
	return result, nil
}

func (l *Library) PredicateTypes(ctx context.Context, q query.Query) ([]string, error) {
	return []string{"author", "tag", "series"}, nil
}

func (l *Library) CustomColumns(ctx context.Context) ([]library.CustomColumn, error) {
	return nil, nil
}

func (l *Library) AddBook(ctx context.Context, epub io.Reader) (library.Book, error) {
	return library.Book{}, nil
}

func (l *Library) UpdateMeta(ctx context.Context, id int64, meta library.BookMeta) error {
	return nil
}

func (l *Library) DeleteBook(ctx context.Context, id int64) error {
	return nil
}

func matches(b book, q query.Query) bool {
	for _, pred := range q {
		switch pred.Type {
		case "author":
			if !contains(b.authors, pred.Value) {
				return false
			}
		case "tag":
			if !contains(b.tags, pred.Value) {
				return false
			}
		case "series":
			if b.series != pred.Value {
				return false
			}
		}
	}
	return true
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
