package library

import "github.com/ramblingenzyme/ebookfs/internal/book"

// Book is a read-only snapshot of a book's state. Library's concurrency
// contract says when a caller needs a fresh one.
type Book = book.ImmutableBook
