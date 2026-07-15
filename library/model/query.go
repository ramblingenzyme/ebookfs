package model

// Query selects a subset of books for search. Within each slice field values are
// OR'd (tag:sci-fi+tag:fantasy matches books with either tag). Across fields they
// are AND'd (tag:sci-fi+status:unread matches books with the tag AND the status).
// Zero-valued fields are ignored. Title is a case-insensitive substring match.
type Query struct {
	Authors []string // books by ANY of these authors (exact name match)
	Tags    []string // books carrying ANY of these tags (exact name match)
	Series  []string // books in ANY of these series (exact name match)
	Status  []string // books with ANY of these statuses (exact match)
	IDs     []int64  // books with ANY of these IDs
	Titles  []string // substring match (case-insensitive)
}
