package library

import (
	"slices"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
)

// The names this package lends its callers. An alias rather than a wrapper, so
// a frontend names one package and the value it holds is the one the internals
// built.

// Book is a read-only snapshot of a book's state. Library's concurrency
// contract says when a caller needs a fresh one.
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

// StatusList renders the reading-status vocabulary, so a validation error and
// the ctl help name the same set.
var StatusList = book.StatusList

// Statuses returns the vocabulary in presentation order. It is a copy, so a
// caller sorting or truncating it cannot reorder the vocabulary process-wide.
func Statuses() []string { return slices.Clone(book.Statuses) }

const (
	OrderSortTitle    = index.OrderSortTitle
	OrderDateAdded    = index.OrderDateAdded
	OrderDateModified = index.OrderDateModified
	OrderRating       = index.OrderRating
	OrderPubdate      = index.OrderPubdate
)

// Exporter produces the rsync-export rendition of a book for the reader/ view.
// It is the single swap point between serving the original epub and a converted
// kepub: the Library returns the appropriate implementation based on config.
//
// Membership and grouping are policy, so they sit with the rendition methods
// here and fs/views/reader.go only renders the result. Includes is a predicate
// rather than an exposed status list, so the policy can change to tag-based or
// size caps without touching the frontend.
type Exporter interface {
	// Open returns a handle to the book's export rendition, holding a snapshot
	// as Book does. The returned reader is non-nil iff err is nil.
	Open(*Book) (EpubReader, error)
	Size(*Book) (int64, bool) // cheap; 9P stat length, false when cold
	Warm(*Book)               // non-blocking proactive warm hint
	Filename(*Book) string    // FAT-safe export name
	Dirname(*Book) string     // FAT-safe export directory name
	Includes(*Book) bool      // whether the book appears in the reader view
}
