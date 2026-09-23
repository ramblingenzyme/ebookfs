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

// toPublication leaves out rating and reading status, since neither has an
// OPDS slot.
//
// filename is the exporter's name for the book, which a converting exporter
// spells differently from b.Filename().
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

	// Sorted, since map order varies per call and a reshuffled feed defeats
	// client caching.
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

// published accepts the three date shapes real epubs carry.
func published(s string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, "2006-01-02", "2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// seriesPosition maps an EPUB 3.3 D.3.7 collection index onto opds.Series's
// float, which renders in OPDS 2.0 only. A multi-level index such as "1.2.3"
// stays zero.
func seriesPosition(index string) float64 {
	pos, err := strconv.ParseFloat(index, 64)
	if err != nil {
		return 0
	}
	return pos
}

// identifierURN gives ISBN and ISSN their registered URN namespaces. Any other
// scheme stays an informal prefix, as the epub's dc:identifier had it.
func identifierURN(scheme, value string) string {
	switch strings.ToLower(scheme) {
	case "isbn", "issn":
		return "urn:" + strings.ToLower(scheme) + ":" + value
	case "":
		return value
	}
	return scheme + ":" + value
}
