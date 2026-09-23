package opds

import (
	"maps"
	"mime"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ophymx/opds"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// toPublication leaves out rating and reading status. Neither has an OPDS slot,
// the status feeds already navigate by status, and nothing consumes a rating.
//
// filename is the exporter's name for the book, which a converting exporter
// spells differently from b.Filename(). It is the one thing rendering needs
// from the exporter, so it arrives as a string rather than behind a Renderer.
func toPublication(b *library.Book, filename string) opds.Publication {
	id := strconv.FormatInt(b.ID(), 10)
	p := opds.NewPublication(urn("book:"+id), b.Title()).
		UpdatedAt(b.DateModified()).
		Summarize(b.Description()).
		Link(opds.RelSelf, publicationPath(id), opds.MediaTypeEntry)
	p.SortAs = b.SortTitle()

	for _, a := range b.Authors() {
		p.Author(opds.Author{
			Name:   a.Name,
			SortAs: a.SortName,
			URI:    feedPath(kindAuthor, a.Name),
		})
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
		p.PartOf(s.Name, seriesPosition(s.Index))
	}

	if cover := b.CoverPath(); cover != "" {
		ct := mime.TypeByExtension(path.Ext(cover))
		p.Cover(coverPath(id), ct).Thumbnail(coverPath(id), ct)
	}

	p.Acquire(opds.Acquisition{
		Rel:  opds.AcquireOpenAccess,
		Href: contentPath(id, filename),
		Type: mediaTypeEpub,
	})

	return *p
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

// seriesPosition maps an EPUB 3.3 D.3.7 collection index onto the float
// opds.Series carries, which renders in OPDS 2.0 only. An index D.3.7 allows
// and a float cannot hold, such as the multi-level "1.2.3", stays at zero
// rather than becoming a number that means something else.
func seriesPosition(index string) float64 {
	pos, err := strconv.ParseFloat(index, 64)
	if err != nil {
		return 0
	}
	return pos
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
