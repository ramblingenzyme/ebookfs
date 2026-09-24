package opds

import (
	"net/url"

	"github.com/ophymx/opds/opdshttp"
)

// Prefix is fixed rather than configured. A deployment that wants the catalog
// elsewhere rewrites the path in its reverse proxy.
const Prefix = "/opds"

// The routes opdshttp does not serve.
const (
	contentBase = Prefix + "/content/"
	coverBase   = Prefix + "/cover/"
)

const mediaTypeEpub = "application/epub+zip"

// valueParam carries a facet value in the query string rather than the feed
// id, so url.Values escapes a tag containing a slash, a colon or a space.
const valueParam = "value"

// urn builds an atom:id from library ids and feed names, so it survives a
// rename, a move and a reindex.
func urn(s string) string { return "urn:ebookfs:" + s }

// feedURN is not derived from feedPath, whose URL path is the same for a
// listing and every feed under it.
func feedURN(kindID, value string) string {
	if value == "" {
		return urn("feed:" + kindID)
	}
	return urn("feed:" + kindID + ":" + value)
}

// feedPath escapes only the value, since kind ids are fixed literals.
func feedPath(kindID, value string) string {
	path := opdshttp.FeedPath(Prefix, kindID)
	if value == "" {
		return path
	}
	return path + "?" + url.Values{valueParam: {value}}.Encode()
}

func publicationPath(id string) string { return opdshttp.PublicationPath(Prefix, id) }

func contentPath(id, filename string) string {
	return contentBase + id + "/" + url.PathEscape(filename)
}

func coverPath(id string) string { return coverBase + id }
