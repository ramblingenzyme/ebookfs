package index

import "time"

// Query OR's the values within a slice field and AND's across fields, so
// tag:sci-fi+tag:fantasy matches either tag while tag:sci-fi+status:unread
// matches both. A zero-valued field is ignored, so Query{} matches every book.
type Query struct {
	Authors []string // display name or sort name
	Tags    []string // exact name
	Series  []string // exact name
	Status  []string // exact
	IDs     []int64
	Titles  []string // case-insensitive substring, unless ExactTitles

	// Exact is binary equality, so it is case-sensitive.
	ExactTitles bool

	Order Order // result ordering; zero value is by sort title
	Limit int   // cap the result count; 0 means no limit
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
