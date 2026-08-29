package library

import "github.com/ramblingenzyme/ebookfs/internal/book"

// Book is an immutable snapshot of a book's state. It is an alias for
// book.ImmutableBook, which wraps the internal book.Book and provides
// read-only access via getters.
//
// A Book is a snapshot at the time it was returned: after an Edit, call
// Library.Content or Search again for updated state.
type Book = book.ImmutableBook
