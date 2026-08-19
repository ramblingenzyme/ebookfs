package model

// Query selects a subset of books. Within each slice field values are OR'd
// (tag:sci-fi+tag:fantasy matches books with either tag). Across fields they
// are AND'd (tag:sci-fi+status:unread matches books with the tag AND the
// status). Zero-valued fields are ignored, so Query{} matches every book.
// Titles are substring matches via SQL LIKE.
type Query struct {
	Authors []string // books by ANY of these authors (matches display name or sort name)
	Tags    []string // books carrying ANY of these tags (exact name match)
	Series  []string // books in ANY of these series (exact name match)
	Status  []string // books with ANY of these statuses (exact match)
	IDs     []int64  // books with ANY of these IDs
	Titles  []string // substring match (case-insensitive) unless ExactTitles

	// ExactTitles matches Titles exactly instead of as substrings. It is the
	// caller's choice, not the query text's: ctl sets it so a selection for a
	// mutating command cannot reach past the title the operator named, while
	// the search view leaves it false because browsing wants the substring.
	// Exact is binary equality (SQL "=", Go "=="), so unlike the substring
	// match it is case-sensitive.
	ExactTitles bool

	Order Order // result ordering; zero value is by sort title
	Limit int   // cap the result count; 0 means no limit
}

// Order selects how Search orders its results. It is presentation, not
// selection: ordering can never change which books match, only the sequence
// they come back in. Limit is the opposite — it changes membership, which is
// why no user-facing query syntax sets it (see ParseQuery).
//
// Each order carries the direction that reads as "best first" for its field:
// dates and ratings descend, titles ascend. A caller wanting the reverse of one
// of these should get its own constant rather than a separate direction flag,
// which would double the combinations to support the two anyone asks for.
type Order int

const (
	OrderSortTitle    Order = iota // by sort title, A-Z; the default
	OrderDateAdded                 // newest addition first
	OrderDateModified              // most recently edited first
	OrderRating                    // highest rated first
	OrderPubdate                   // most recently published first
)
