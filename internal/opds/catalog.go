package opds

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/ophymx/opds"
	"github.com/ophymx/opds/opdshttp"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// OPDS clients follow rel="next", so pageSize bounds one response rather than
// the result set.
const pageSize = 50

// catalog implements opds.Source and opds.Searcher over the library.
//
// A feed id is a kind id from the kinds table, and the facet value rides in
// the query string beside it. The bare id is the kind's listing, or its books
// when the kind has no listing.
type catalog struct {
	lib      Library
	filename func(*library.Book) string // Renderer.Filename; all a feed needs of the exporter
	base     string                     // the configured absolute base URL, or ""
}

func (c *catalog) Root(_ context.Context, _ opds.FeedRequest) (*opds.Feed, error) {
	f := opds.NewFeed(urn("catalog"), "ebookfs")
	for _, k := range kinds {
		f.AddNav(k.title, feedPath(k.id, ""), k.mediaType(), k.rel)
	}
	return f, nil
}

func (c *catalog) Feed(_ context.Context, req opds.FeedRequest) (*opds.Feed, error) {
	k, ok := lookupKind(req.ID)
	if !ok {
		return nil, opds.ErrNotFound
	}
	value := req.Query.Get(valueParam)
	if value == "" && k.list != nil {
		return c.navFeed(k)
	}
	// A value against a kind that has no values to browse, such as
	// feed/all?value=x.
	if value != "" && k.list == nil {
		return nil, opds.ErrNotFound
	}
	return c.bookFeed(req, k, value)
}

// Publication serves the standalone entry document for one book. id is the
// book id in decimal, as publicationPath writes it.
func (c *catalog) Publication(_ context.Context, id string) (*opds.Publication, error) {
	b, err := lookupBook(c.lib, id)
	if isNotFound(err) {
		return nil, opds.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p := toPublication(b, c.filename(b))
	return &p, nil
}

// Search maps an OpenSearch request onto a library query. Terms match the
// title as a substring, which is the library's own default for a bare search
// (see index.Query.Titles).
//
// ponytail: terms never reach the author field, since Query ANDs across fields
// and cannot express "title OR author" in one call. Lift it with an
// index-level OR rather than a union assembled here.
func (c *catalog) Search(_ context.Context, req opds.SearchRequest) (*opds.Feed, error) {
	var q library.Query
	for _, t := range []string{req.Terms, req.Title} {
		if t = strings.TrimSpace(t); t != "" {
			q.Titles = append(q.Titles, t)
		}
	}
	if a := strings.TrimSpace(req.Author); a != "" {
		q.Authors = append(q.Authors, a)
	}
	if len(q.Titles) == 0 && len(q.Authors) == 0 {
		return nil, opds.ErrNotFound
	}

	books, err := c.lib.Search(q)
	if err != nil {
		return nil, err
	}
	// Titled from the query rather than the request, so the terms appear once
	// each and separated by one space however the client spelled them.
	f := opds.NewFeed(urn("search"), "Search: "+strings.Join(slices.Concat(q.Titles, q.Authors), " "))
	return c.page(f, opdshttp.SearchPath(Prefix), req.Page, books), nil
}

// SearchDescription is the one document whose URL must be absolute (OpenSearch
// 1.1 §4.4, "Url/@template"), and the handler absolutizes only a template it
// built itself. With no base configured the relative template is all there is,
// and clients resolve it against the document's own URL.
func (c *catalog) SearchDescription() opds.SearchDescription {
	return opds.SearchDescription{
		ShortName:   "ebookfs",
		Description: "Search the ebookfs library by title or author",
		Template:    c.base + opdshttp.SearchPath(Prefix) + "?q={searchTerms}&author={author?}&title={title?}",
	}
}

// navFeed keeps the order the listing returned.
func (c *catalog) navFeed(k kind) (*opds.Feed, error) {
	values, err := k.list(c.lib)
	if err != nil {
		return nil, err
	}
	f := opds.NewFeed(feedURN(k.id, ""), k.title)
	for _, v := range values {
		e := opds.NavEntry{
			Title: v.Name,
			Href:  feedPath(k.id, v.Name),
			Type:  opds.MediaTypeAcquisition,
			Rel:   opds.RelSubsection,
		}
		// The count rides along as the entry's description, the only place
		// OPDS 1.2 navigation has for it. A zero count is the fixed status
		// vocabulary, which has none to report.
		if v.Count > 0 {
			e.Content = plural(v.Count, "book")
		}
		f.AddNavEntry(e)
	}
	return f, nil
}

// bookFeed titles itself after the value it narrows to, falling back to the
// kind's own title when it narrows to nothing.
func (c *catalog) bookFeed(req opds.FeedRequest, k kind, value string) (*opds.Feed, error) {
	books, err := c.lib.Search(k.query(value))
	if err != nil {
		return nil, err
	}
	title := k.title
	if value != "" {
		title = value
	}
	f := opds.NewFeed(feedURN(k.id, value), title)
	return c.page(f, feedPath(k.id, value), req.Page, books), nil
}

// ponytail: this slices one page out of every matching book the caller already
// loaded, so page 20 costs what page 1 does. Add an Offset to index.Query if a
// library ever grows enough to feel it.
func (c *catalog) page(f *opds.Feed, baseHref string, reqPage int, books []*library.Book) *opds.Feed {
	page := max(reqPage, 1)
	start := min((page-1)*pageSize, len(books))
	end := min(start+pageSize, len(books))
	for _, b := range books[start:end] {
		f.Add(toPublication(b, c.filename(b)))
	}
	f.Page(len(books), pageSize, start+1)
	return f.Paged(baseHref, page, end < len(books))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
