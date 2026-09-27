package library

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
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
type Stats = index.Stats
type Facet = index.Facet
type Author = book.Author
type Series = book.SeriesRef
type EpubReader = epub.EpubReader

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

// Exporter produces a book's rendition for the reader/ view: the original epub
// or a converted kepub, chosen by config.
//
// Which books appear and how they group is policy, so it lives here and the
// frontend only renders it. Includes is a predicate rather than a status list,
// so the policy can change without touching the frontend.
type Exporter interface {
	// Open returns a handle to the rendition, a snapshot as Book is.
	Open(*Book) (EpubReader, error)
	Size(*Book) (int64, bool) // never converts; false when the size is unknown
	Warm(*Book)               // non-blocking hint to convert ahead of a read
	Filename(*Book) string    // FAT-safe export name
	Dirname(*Book) string     // FAT-safe export directory name
	Includes(*Book) bool      // whether the book appears in reader/
}
