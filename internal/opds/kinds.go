package opds

import (
	"github.com/ophymx/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// recentLimit holds the recently-added feed to a single page. A "what's new"
// feed that paginates through the whole library is the all-books feed
// reordered.
const recentLimit = pageSize

// kindAuthor is named because the entry renderer links each author back to
// their feed, which is the one id used outside this table.
const kindAuthor = "author"

// kind is one axis the catalog can be browsed by. Root renders this table and
// Feed dispatches on it, so a kind reachable by URL but absent from the root
// feed is not a state this package can reach.
type kind struct {
	id    string // URL segment, and the atom:id fragment feedURN builds from
	title string
	rel   string // the relation Root's link to it carries

	// list returns the kind's distinct values. A nil list means the kind has
	// no listing to browse: its id alone addresses an acquisition feed.
	list func(Library) ([]library.Facet, error)

	// query selects the books behind one value, or behind the kind itself when
	// list is nil and the value is empty.
	query func(value string) library.Query
}

// kinds are ordered, since Root lists them in this order.
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

// mediaType is the type of the feed a kind's own id addresses: a listing for a
// kind that has one, the books themselves for a kind that does not.
func (k kind) mediaType() string {
	if k.list == nil {
		return opds.MediaTypeAcquisition
	}
	return opds.MediaTypeNavigation
}

// statusFacets lists the reading-status vocabulary, which is fixed rather than
// drawn from the library: a status with no books is still a status, and the
// empty feed behind it says so. The counts stay zero, and navFeed shows no
// count rather than claiming none exist.
func statusFacets(Library) ([]library.Facet, error) {
	statuses := library.Statuses()
	out := make([]library.Facet, len(statuses))
	for i, s := range statuses {
		out[i] = library.Facet{Name: s}
	}
	return out, nil
}
