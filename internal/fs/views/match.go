package views

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Membership for a search handle, evaluated in Go against a book snapshot.
// index.Search answers the same Query in SQL; search_parity_scenario_test.go
// keeps the two agreeing.

// Each matcher answers for one query field. An empty field imposes no
// constraint and matches everything. Without that guard Query{} matches
// nothing.

// matchesAuthors matches any of q.Authors against either author column, as
// Index.Search does in SQL.
func matchesAuthors(q library.Query, b *library.Book) bool {
	return len(q.Authors) == 0 || slices.ContainsFunc(b.Authors(), func(a library.Author) bool {
		return slices.Contains(q.Authors, a.Name) || slices.Contains(q.Authors, a.SortName)
	})
}

func matchesTags(q library.Query, b *library.Book) bool {
	return len(q.Tags) == 0 || slices.ContainsFunc(q.Tags, func(t string) bool {
		return slices.Contains(b.Tags(), t)
	})
}

func matchesSeries(q library.Query, b *library.Book) bool {
	return len(q.Series) == 0 || (b.HasSeries() && slices.Contains(q.Series, b.SeriesName()))
}

func matchesStatus(q library.Query, b *library.Book) bool {
	return len(q.Status) == 0 || slices.Contains(q.Status, b.Status())
}

func matchesIDs(q library.Query, b *library.Book) bool {
	return len(q.IDs) == 0 || slices.Contains(q.IDs, b.ID())
}

// matchesTitles matches any of q.Titles against the book's title: exactly when
// q.ExactTitles, otherwise as a case-insensitive substring.
func matchesTitles(q library.Query, b *library.Book) bool {
	if len(q.Titles) == 0 {
		return true
	}
	if q.ExactTitles {
		return slices.Contains(q.Titles, b.Title())
	}
	lower := strings.ToLower(b.Title())
	return slices.ContainsFunc(q.Titles, func(title string) bool {
		return strings.Contains(lower, strings.ToLower(title))
	})
}

// makeMatchesFn returns a predicate reporting whether a book matches q. Values
// within a field are OR'd, inside each matcher; fields are AND'd, in the chain
// below.
//
// The predicate is the single membership authority for a search handle.
// ResyncView replays every registered book through it at query time, and
// registry events evaluate it for live updates, so both paths agree by
// construction.
//
// Only the selecting fields are honoured. Query.Order cannot matter: a
// directory is keyed by name, and ordering never changes membership.
//
// Query.Limit would matter, since capping a result set changes which books are
// in it. textfmt.ParseQuery has no syntax that sets it and must not grow one.
// ctl shares that parser, where a limited selection would mutate an arbitrary
// subset of the books the operator named.
func makeMatchesFn(q library.Query) func(*library.Book) bool {
	return func(b *library.Book) bool {
		return matchesAuthors(q, b) && matchesTags(q, b) && matchesSeries(q, b) &&
			matchesStatus(q, b) && matchesIDs(q, b) && matchesTitles(q, b)
	}
}
