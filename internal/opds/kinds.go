package opds

import (
	"github.com/ophymx/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// recentLimit caps the recent feed at one page. Paginated, it would be the
// all-books feed reordered.
const recentLimit = pageSize

const kindAuthor = "author"

// kind is one axis the catalog can be browsed by. Root and Feed both read the
// kinds table, so every kind a URL reaches is listed in the root feed.
type kind struct {
	id    string // URL segment, and the atom:id fragment feedURN builds from
	title string
	rel   string // the relation Root's link to it carries

	// list returns the kind's distinct values. Nil means the id addresses an
	// acquisition feed directly.
	list func(Library) ([]library.Facet, error)

	// query selects the books behind one value, or behind the kind itself when
	// list is nil and the value is empty.
	query func(value string) library.Query
}

// kinds is in the order Root lists them.
var kinds = []kind{
	{
		id: "all", title: "All Books", rel: opds.RelSubsection,
		query: func(string) library.Query { return library.Query{} },
	},
	{
		id: "recent", title: "Recently Added", rel: opds.RelSortNew,
		query: func(string) library.Query {
			return library.Query{Order: library.OrderDateAdded, Limit: recentLimit}
		},
	},
	{
		id: kindAuthor, title: "Authors", rel: opds.RelSubsection,
		list:  Library.Authors,
		query: func(v string) library.Query { return library.Query{Authors: []string{v}} },
	},
	{
		id: "series", title: "Series", rel: opds.RelSubsection,
		list:  Library.Series,
		query: func(v string) library.Query { return library.Query{Series: []string{v}} },
	},
	{
		id: "tag", title: "Tags", rel: opds.RelSubsection,
		list:  Library.Tags,
		query: func(v string) library.Query { return library.Query{Tags: []string{v}} },
	},
	{
		id: "status", title: "Reading Status", rel: opds.RelSubsection,
		list:  statusFacets,
		query: func(v string) library.Query { return library.Query{Status: []string{v}} },
	},
}

func lookupKind(id string) (kind, bool) {
	for _, k := range kinds {
		if k.id == id {
			return k, true
		}
	}
	return kind{}, false
}

func (k kind) mediaType() string {
	if k.list == nil {
		return opds.MediaTypeAcquisition
	}
	return opds.MediaTypeNavigation
}

// statusFacets lists the fixed status vocabulary, so a status no book holds
// still gets a feed. Counts stay zero, which navFeed shows as no count.
func statusFacets(Library) ([]library.Facet, error) {
	statuses := library.Statuses()
	out := make([]library.Facet, len(statuses))
	for i, s := range statuses {
		out[i] = library.Facet{Name: s}
	}
	return out, nil
}
