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

	Recent bool // order by date added (newest first) instead of sort title
	Limit  int  // cap the result count; 0 means no limit
}
