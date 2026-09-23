package opds

import (
	"net/url"

	"github.com/ophymx/opds/opdshttp"
)

// Prefix is where the catalog is mounted. It is fixed rather than configured:
// a deployment that wants it elsewhere rewrites the path in its reverse proxy,
// and every href the catalog emits is built from this one constant.
const Prefix = "/opds"

// The two routes the OPDS handler does not serve. Each segment is spelled
// once, so the pattern NewHandler registers and the href the entry renderer
// builds cannot drift apart.
const (
	contentBase = Prefix + "/content/"
	coverBase   = Prefix + "/cover/"
)

const mediaTypeEpub = "application/epub+zip"

// valueParam carries a facet value. Holding it in the query string rather than
// in the feed id leaves the encoding to url.Values, so a tag containing a
// slash, a colon or a space needs no escaping rule of the catalog's own.
const valueParam = "value"

// urn builds a stable atom:id. It is derived from the library's own ids and
// feed names, so it survives a rename, a move and a reindex.
func urn(s string) string { return "urn:ebookfs:" + s }

// feedURN identifies a kind's listing, or the feed behind one of its values.
// An atom:id is opaque and never resolved, so it keeps the value joined with a
// colon even though feedPath no longer does. Deriving it from the path instead
// would give a listing and every feed under it the same id, since they share
// one path.
func feedURN(kindID, value string) string {
	if value == "" {
		return urn("feed:" + kindID)
	}
	return urn("feed:" + kindID + ":" + value)
}

// feedPath is the href of a kind's listing, or of the feed behind one of its
// values. Kind ids are fixed literals, so only the value needs encoding.
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
