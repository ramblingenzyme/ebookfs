package opf

import (
	"strings"
	"time"
)

// The fields whose whole encoding fits in a few lines. The three with more to
// say — authors, series, cover — keep a file each.

type titleField struct{ o *Doc }

func (o *Doc) title() titleField { return titleField{o} }

func (f titleField) element() *elementSlot { return f.o.dcElement("title", "ebookfs-title") }

// get returns the title and its sort value, which is the EPUB 3 file-as refine
// on the title element. EPUB 2 has no standard mechanism for it.
func (f titleField) get() (title, sort string) {
	el := f.element()
	return el.get(), el.refine("file-as").get()
}

func (f titleField) set(title, sort *string) {
	el := f.element()
	if title != nil {
		el.set(*title)
	}

	// A v2 package gets no sort title: no standard mechanism and, unlike series,
	// no proprietary fallback either. Neither does a file with no title element
	// to refine — a sort title alone is not reason enough to invent one.
	if !f.o.epub3() || !el.exists() || (title == nil && sort == nil) {
		return
	}

	// A title written without one drops the sort title it used to carry.
	value := ""
	if sort != nil {
		value = collapse(*sort)
	}
	put(el.refine("file-as"), value)
}

type modifiedField struct{ o *Doc }

func (o *Doc) modified() modifiedField { return modifiedField{o} }

// set records the time of this rewrite: exactly one unrefined dcterms:modified
// per §5.5.5, in the UTC format §5.5.4 fixes. Write-only, and EPUB 3 only.
func (f modifiedField) set(t time.Time) {
	if !f.o.epub3() {
		return
	}
	f.o.propertyMeta("dcterms:modified").set(t.UTC().Format("2006-01-02T15:04:05Z"))
}

// description and language are repeatable (§5.5.3.2.1) but single-valued to us,
// and have no encoding of their own, so they get no field type: dcElement picks
// the element a read and a write both mean.
func (o *Doc) description() string { return o.dcElement("description", "").get() }

func (o *Doc) language() string { return o.dcElement("language", "").get() }

// identifiers keys by the element's XML id, which is the wrong key and frozen
// that way: changing it is a schema migration, and
// TestIdentifiersKeepTheirCurrentKeying guards it. Read-only.
func (o *Doc) identifiers() map[string]string {
	out := map[string]string{}
	for _, el := range o.elements("identifier") {
		out[attr(el, "id")] = text(el)
	}
	return out
}

// pubdate returns a <dc:date> verbatim, never parsed. An EPUB 2 opf:event
// picks it: "publication" is authoritative, and any other event means the file
// saying this is not the publication date, leaving the untagged elements.
// Exactly one of those is used; zero or several leaves the date unset.
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
