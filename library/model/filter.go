package model

// Filter selects a subset of books for Query. Zero-valued fields are ignored,
// so Filter{} matches every book. String fields match by exact name.
type Filter struct {
	ID     int64  // a single book by id
	Author string // books by an author's name
	Tag    string // books carrying a tag
	Status string // books with a reading status
	Series string // books in a series
	Recent bool   // order by date added (newest first) instead of sort title
	Limit  int    // cap the result count; 0 means no limit
}
