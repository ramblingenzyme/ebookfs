package library

import (
	"context"
	"io"

	"github.com/ramblingenzyme/ebookfs/internal/query"
)

type Library interface {
	Books(ctx context.Context, q query.Query) ([]Book, error)
	Values(ctx context.Context, predicateType string, q query.Query) ([]string, error)
	PredicateTypes(ctx context.Context, q query.Query) ([]string, error)
	CustomColumns(ctx context.Context) ([]CustomColumn, error)
	AddBook(ctx context.Context, epub io.Reader) (Book, error)
	UpdateMeta(ctx context.Context, id int64, meta BookMeta) error
	DeleteBook(ctx context.Context, id int64) error
}

type Book struct {
	ID           int64
	Title        string
	EpubPath     string
	CoverPath    string
	MetadataPath string
}

type CustomColumn struct {
	Name  string
	Label string
}

type BookMeta struct {
	Title     *string  `json:"title,omitempty"`
	Authors   []string `json:"authors,omitempty"`
	Tags      []string `json:"tags,omitempty"`
	Series    *string  `json:"series,omitempty"`
	Publisher *string  `json:"publisher,omitempty"`
	Comments  *string  `json:"comments,omitempty"`
}
