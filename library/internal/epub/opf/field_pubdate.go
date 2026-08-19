package opf

import "strings"

// pubdate resolves the publication date from the <dc:date> elements. In EPUB 2
// these may be tagged with an opf:event ("publication"/"creation"/"modification"
// — the spec's example vocabulary). Publication-date selection is independent of
// parseability — the raw value is returned as-is.
//
//  1. A designated opf:event="publication" date is authoritative (first match
//     if a malformed file declares several) and is returned verbatim.
//  2. Otherwise every evented date is the file declaring "this is not the
//     publication date", leaving only untagged <dc:date>. Exactly one untagged
//     date is used; zero or several leaves the date unset.
//
// EPUB 3 carries a single untagged <dc:date> (last-modified lives in a separate
// <meta property="dcterms:modified">, not a <dc:date>, so it never reaches here),
// falling through to the step-2 single-date case. Empty <dc:date> elements are
// ignored throughout.
//
// Read-only: nothing in ebookfs writes the publication date.
func (o *Doc) pubdate() string {
	var (
		untagged string
		count    int
	)
	for _, d := range o.elements("date") {
		val := text(d)
		if val == "" {
			continue
		}
		event := attr(d, "event")
		if strings.ToLower(event) == "publication" {
			return val
		}
		if event == "" {
			count++
			untagged = val
		}
	}
	if count == 1 {
		return untagged
	}
	return ""
}
