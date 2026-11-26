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

type (
	Edits           = book.Edits
	ValidationError = book.ValidationError
	FieldError      = book.FieldError
	Query           = index.Query
	Order           = index.Order
)

type (
	Bib         = book.Bib
	Stats       = index.Stats
	Facet       = index.Facet
	Author      = book.Author
	Contributor = book.Contributor
	Series      = book.SeriesRef
	Format      = book.Format
)

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
