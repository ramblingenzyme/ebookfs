package index

import (
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// Query OR's the values within a filter and AND's across filters, so
// tag:sci-fi+tag:fantasy matches either tag while tag:sci-fi+status:unread
// matches both. A zero-valued filter is ignored, so Query{} matches every book.
//
// Every field but Order and Limit is a filter, and must be returned by
// filters.
type Query struct {
	Authors   AuthorFilter
	Tags      TagFilter
	Series    SeriesFilter
	Status    StatusFilter
	Subjects  SubjectFilter
	Publisher PublisherFilter
	Language  LanguageFilter
	IDs       IDFilter
	Titles    TitleFilter

	Order Order // result ordering; zero value is by sort title
	Limit int   // cap the result count; 0 means no limit
}

func (q Query) filters() []filter {
	return []filter{q.Authors, q.Tags, q.Series, q.Status, q.Subjects, q.Publisher, q.Language, q.IDs, q.Titles}
}

// Matches answers q against a snapshot in Go, for a caller filtering books it
// already holds. Search answers the same query in SQL, and
// search_parity_scenario_test.go keeps the two agreeing.
//
// Only the filters count: Order and Limit are ignored, since they shape a
// result set rather than decide one book's membership.
func (q Query) Matches(b *book.ImmutableBook) bool {
	for _, f := range q.filters() {
		if !f.matches(b) {
			return false
		}
	}

	return true
}

// Order is presentation: it can never change which books match, only the
// sequence they come back in. Limit does change membership, which is why no
// user-facing query syntax sets it (see ParseQuery).
//
// Each order carries the direction that reads as "best first" for its field:
// dates and ratings descend, titles ascend.
type Order int

const (
	OrderSortTitle    Order = iota // by sort title, A-Z; the default
	OrderDateAdded                 // newest addition first
	OrderDateModified              // most recently edited first
	OrderRating                    // highest rated first
	OrderPubdate                   // most recently published first
)

type Facet struct {
	Name  string
	Count int
}

type Stats struct {
	Books        int
	Authors      int
	Series       int
	Tags         int
	TotalSize    int64
	LastAdded    time.Time
	LastModified time.Time
}
