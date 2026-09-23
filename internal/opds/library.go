package opds

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type Library interface {
	Search(library.Query) ([]*library.Book, error)
	Get(int64) (*library.Book, error)
	Content(int64) (library.EpubReader, error)
	Authors() ([]library.Facet, error)
	Series() ([]library.Facet, error)
	Tags() ([]library.Facet, error)
}

// Renderer is the three library.Exporter methods that deliver a book's bytes.
// Includes and Dirname are absent because they decide reader/ membership and
// grouping, and the catalog serves the whole library under its own navigation.
type Renderer interface {
	Open(*library.Book) (library.EpubReader, error)
	Size(*library.Book) (int64, bool)
	Filename(*library.Book) string
}

// lookupBook hands back the library's own error, so each layer above
// translates once at its own edge: Publication into opds.ErrNotFound, and
// httpError into a status. An id that is not a number is a book the library
// does not hold, which is the same answer.
func lookupBook(lib Library, id string) (*library.Book, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("book %q: %w", id, library.ErrBookNotFound)
	}
	return lib.Get(n)
}

// isNotFound covers a book with no cover as well as one the index does not
// hold: a coverless book is ordinary, so its cover route answers 404, not 500.
func isNotFound(err error) bool {
	return errors.Is(err, library.ErrBookNotFound) || errors.Is(err, library.ErrNoCover)
}
