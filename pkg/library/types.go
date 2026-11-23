package library

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/formats"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
)

// Book is a read-only snapshot of a book. Library's doc says when a caller
// needs a fresh one.
type Book = book.ImmutableBook

type Edits = book.Edits
type ValidationError = book.ValidationError
type FieldError = book.FieldError
type Query = index.Query
type Order = index.Order

type Bib = book.Bib
type Stats = index.Stats
type Facet = index.Facet
type Author = book.Author
type Contributor = book.Contributor
type Series = book.SeriesRef
type Format = book.Format

const FormatEPUB = book.FormatEPUB

// Reader is an open book file. It shows the book as it was when opened.
type Reader = formats.Reader

// StatusList renders the reading statuses, so a validation error and the ctl
// help name the same set.
var StatusList = book.StatusList

// Statuses returns the reading statuses in presentation order. It returns a
// copy, so a caller sorting it cannot reorder them for everyone.
func Statuses() []string { return slices.Clone(book.Statuses) }

const (
	OrderSortTitle    = index.OrderSortTitle
	OrderDateAdded    = index.OrderDateAdded
	OrderDateModified = index.OrderDateModified
	OrderRating       = index.OrderRating
	OrderPubdate      = index.OrderPubdate
)
