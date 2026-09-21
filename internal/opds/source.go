package opds

import (
	"context"
	"fmt"
	"maps"
	"mime"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/opds"
	"github.com/ophymx/opds/opdshttp"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// OPDS clients follow rel="next", so pageSize bounds one response rather than
// the result set.
const pageSize = 50

// recentLimit holds the recently-added feed to a single page. A "what's new"
// feed that paginates through the whole library is the all-books feed
// reordered.
const recentLimit = pageSize

const mediaTypeEpub = "application/epub+zip"

// catalog implements opds.Source and opds.Searcher over the library.
//
// Feed ids are "kind" or "kind:value": the bare kind is the navigation feed
// listing that field's values, and a value makes it the acquisition feed of
// the books carrying it. Only the first colon separates, so a value may hold
// one. A value holding a slash survives because the href escapes it and the
// handler hands back the decoded path.
type catalog struct {
	lib  Library
	rend Renderer
	base string // the configured absolute base URL, or ""
}

func (c *catalog) Root(_ context.Context, _ opds.FeedRequest) (*opds.Feed, error) {
	return opds.NewFeed(urn("catalog"), "ebookfs").
		AddNav("All Books", feedPath("all"), opds.MediaTypeAcquisition, opds.RelSubsection).
		AddNav("Recently Added", feedPath("recent"), opds.MediaTypeAcquisition, opds.RelSortNew).
		AddNav("Authors", feedPath("author"), opds.MediaTypeNavigation, opds.RelSubsection).
		AddNav("Series", feedPath("series"), opds.MediaTypeNavigation, opds.RelSubsection).
		AddNav("Tags", feedPath("tag"), opds.MediaTypeNavigation, opds.RelSubsection).
		AddNav("Reading Status", feedPath("status"), opds.MediaTypeNavigation, opds.RelSubsection), nil
}

func (c *catalog) Feed(_ context.Context, req opds.FeedRequest) (*opds.Feed, error) {
	kind, value, _ := strings.Cut(req.ID, ":")
	switch {
	case kind == "all" && value == "":
		return c.books(req, "All Books", library.Query{})
	case kind == "recent" && value == "":
		return c.books(req, "Recently Added", library.Query{Order: library.OrderDateAdded, Limit: recentLimit})
	case kind == "author" && value == "":
		return c.facets("Authors", "author", c.lib.Authors)
	case kind == "series" && value == "":
		return c.facets("Series", "series", c.lib.Series)
	case kind == "tag" && value == "":
		return c.facets("Tags", "tag", c.lib.Tags)
	case kind == "status" && value == "":
		return c.statuses(), nil
	case kind == "author":
		return c.books(req, value, library.Query{Authors: []string{value}})
	case kind == "series":
		return c.books(req, value, library.Query{Series: []string{value}})
	case kind == "tag":
		return c.books(req, value, library.Query{Tags: []string{value}})
	case kind == "status":
		return c.books(req, value, library.Query{Status: []string{value}})
	}
	return nil, opds.ErrNotFound
}

// Publication serves the standalone entry document for one book. id is the
// book id in decimal, as publicationPath writes it.
func (c *catalog) Publication(_ context.Context, id string) (*opds.Publication, error) {
	b, err := c.book(id)
	if err != nil {
		return nil, err
	}
	p := c.publication(b)
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
	if t := strings.TrimSpace(req.Terms); t != "" {
		q.Titles = append(q.Titles, t)
	}
	if t := strings.TrimSpace(req.Title); t != "" {
		q.Titles = append(q.Titles, t)
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
	title := fmt.Sprintf("Search: %s", strings.TrimSpace(req.Terms+" "+req.Title+" "+req.Author))
	f := opds.NewFeed(urn("search"), title)
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

func (c *catalog) books(req opds.FeedRequest, title string, q library.Query) (*opds.Feed, error) {
	books, err := c.lib.Search(q)
	if err != nil {
		return nil, err
	}
	f := opds.NewFeed(urn("feed:"+req.ID), title)
	return c.page(f, opdshttp.FeedPath(Prefix, req.ID), req.Page, books), nil
}

// ponytail: this slices one page out of every matching book the caller already
// loaded, so page 20 costs what page 1 does. Add an Offset to index.Query if a
// library ever grows enough to feel it.
func (c *catalog) page(f *opds.Feed, baseHref string, reqPage int, books []*library.Book) *opds.Feed {
	page := max(reqPage, 1)
	start := min((page-1)*pageSize, len(books))
	end := min(start+pageSize, len(books))
	for _, b := range books[start:end] {
		f.Add(c.publication(b))
	}
	f.Page(len(books), pageSize, start+1)
	return f.Paged(baseHref, page, end < len(books))
}

// facets keeps the order the index returned. The count rides along as the
// entry's description, the only place OPDS 1.2 navigation has for it.
func (c *catalog) facets(title, kind string, list func() ([]library.Facet, error)) (*opds.Feed, error) {
	values, err := list()
	if err != nil {
		return nil, err
	}
	f := opds.NewFeed(urn("feed:"+kind), title)
	for _, v := range values {
		f.AddNavEntry(opds.NavEntry{
			Title:   v.Name,
			Content: plural(v.Count, "book"),
			Href:    feedPath(kind + ":" + v.Name),
			Type:    opds.MediaTypeAcquisition,
			Rel:     opds.RelSubsection,
		})
	}
	return f, nil
}

// statuses lists a fixed vocabulary rather than querying the library: a status
// with no books is still a status, and the empty feed behind it says so.
func (c *catalog) statuses() *opds.Feed {
	f := opds.NewFeed(urn("feed:status"), "Reading Status")
	for _, s := range library.Statuses() {
		f.AddNav(s, feedPath("status:"+s), opds.MediaTypeAcquisition, opds.RelSubsection)
	}
	return f
}

// publication leaves out rating and reading status. Neither has an OPDS slot,
// the status feeds already navigate by status, and nothing consumes a rating.
func (c *catalog) publication(b *library.Book) opds.Publication {
	id := strconv.FormatInt(b.ID(), 10)
	p := opds.NewPublication(urn("book:"+id), b.Title()).
		UpdatedAt(b.DateModified()).
		Summarize(b.Description()).
		Link(opds.RelSelf, publicationPath(id), opds.MediaTypeEntry)
	p.SortAs = b.SortTitle()

	for _, a := range b.Authors() {
		p.Author(opds.Author{Name: a.Name, SortAs: a.SortName, URI: feedPath("author:" + a.Name)})
	}
	if l := b.Language(); l != "" {
		p.In(l)
	}
	if t, ok := published(b.Pubdate()); ok {
		p.PublishedAt(t)
	}
	// Sorted by scheme, since the map's own order varies per call and a feed
	// that reshuffles between requests defeats a client's caching.
	ids := b.Identifiers()
	for _, scheme := range slices.Sorted(maps.Keys(ids)) {
		p.Identifier(identifierURN(scheme, ids[scheme]))
	}
	for _, t := range b.Tags() {
		p.About(t)
	}
	if s := b.Series(); s != nil {
		// opds.Series renders in OPDS 2.0 only, and its Position is a float.
		// EPUB 3.3 D.3.7 indexes are not: "98.4" there means volume 98, issue
		// 4. A multi-level index stays at zero rather than becoming a number
		// that means something else.
		pos, err := strconv.ParseFloat(s.Index, 64)
		if err != nil {
			pos = 0
		}
		p.PartOf(s.Name, pos)
	}
	if cover := b.CoverPath(); cover != "" {
		ct := mime.TypeByExtension(path.Ext(cover))
		p.Cover(coverPath(id), ct).Thumbnail(coverPath(id), ct)
	}
	p.Acquire(opds.Acquisition{
		Rel:  opds.AcquireOpenAccess,
		Href: contentPath(id, c.rend.Filename(b)),
		Type: mediaTypeEpub,
	})
	return *p
}

func (c *catalog) book(id string) (*library.Book, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return nil, opds.ErrNotFound
	}
	b, err := c.lib.Get(n)
	if err != nil {
		if isNotFound(err) {
			return nil, opds.ErrNotFound
		}
		// An index failure, which the handler's default mapping turns into a 500.
		return nil, err
	}
	return b, nil
}

// published accepts a full timestamp, a date, or a year alone, the three
// shapes real epubs carry. Anything else is dropped rather than guessed at.
func published(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// identifierURN renders an identifier as the URN OPDS expects. ISBN and ISSN
// have registered URN namespaces. Anything else keeps its scheme as an
// informal prefix, which is what the epub's own dc:identifier carried.
func identifierURN(scheme, value string) string {
	switch strings.ToLower(scheme) {
	case "isbn", "issn":
		return "urn:" + strings.ToLower(scheme) + ":" + value
	case "":
		return value
	}
	return scheme + ":" + value
}

// urn builds a stable atom:id. It is derived from the library's own ids and
// feed names, so it survives a rename, a move and a reindex.
func urn(s string) string { return "urn:ebookfs:" + s }

// feedPath is opdshttp.FeedPath with the id escaped, since a facet value may
// hold a slash or a space.
func feedPath(id string) string { return opdshttp.FeedPath(Prefix, url.PathEscape(id)) }

func publicationPath(id string) string { return opdshttp.PublicationPath(Prefix, id) }

func contentPath(id, filename string) string {
	return Prefix + "/content/" + id + "/" + url.PathEscape(filename)
}

func coverPath(id string) string { return Prefix + "/cover/" + id }

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
