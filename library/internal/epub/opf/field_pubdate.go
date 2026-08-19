package opf

import "strings"

// pubdate returns a <dc:date> verbatim, never parsed. An EPUB 2 opf:event
// picks it: "publication" is authoritative, and any other event means the file
// saying this is not the publication date, leaving the untagged elements.
// Exactly one of those is used; zero or several leaves the date unset.
//
// Read-only.
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
