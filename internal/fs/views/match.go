package views

import (
	"slices"
	"strings"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// Each matcher answers for one query field against a book snapshot. An empty
// field imposes no constraint and matches everything; without that guard
// Query{} matches nothing.
//
// index.Search answers the same Query in SQL. search_parity_scenario_test.go
// keeps the two agreeing.

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

// makeMatchesFn returns a predicate matching a book against q: values within a
// field OR'd, fields AND'd.
//
// It is the single membership authority for a search handle. ResyncView replays
// every registered book through it at query time and registry events evaluate
// it live, so both paths agree by construction.
//
// Only the selecting fields count. Query.Order cannot matter, since a directory
// is keyed by name. Query.Limit would, so textfmt.ParseQuery has no syntax that
// sets it and must not grow one: ctl shares that parser, where a limited
// selection would mutate an arbitrary subset of the books the operator named.
func makeMatchesFn(q library.Query) func(*library.Book) bool {
	return func(b *library.Book) bool {
		return matchesAuthors(q, b) && matchesTags(q, b) && matchesSeries(q, b) &&
			matchesStatus(q, b) && matchesIDs(q, b) && matchesTitles(q, b)
	}
}
