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

type Renderer interface {
	Open(*library.Book) (library.EpubReader, error)
	Size(*library.Book) (int64, bool)
	Filename(*library.Book) string
}

// lookupBook returns the library's error untranslated, so Publication and
// httpError each map it at their own edge.
func lookupBook(lib Library, id string) (*library.Book, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("book %q: %w", id, library.ErrBookNotFound)
	}
	return lib.Get(n)
}

func isNotFound(err error) bool {
	return errors.Is(err, library.ErrBookNotFound) || errors.Is(err, library.ErrNoCover)
}
