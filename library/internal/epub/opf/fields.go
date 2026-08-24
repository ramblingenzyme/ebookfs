package opf

import (
	"strings"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The fields whose whole encoding fits in a few lines. The three with more to
// say — authors, series, cover — keep a file each.

type titleField struct{ o *Doc }

func (o *Doc) title() titleField { return titleField{o} }

func (f titleField) element() *elementSlot { return f.o.dcElement("title", "ebookfs-title") }

// The sort title has the same shape as the series: a standard EPUB 3 mechanism,
// and for EPUB 2, which has none, the proprietary meta calibre writes. Checked
// against calibre itself — `ebook-meta --title-sort` writes a file-as refinement
// into a v3 package and calibre:title_sort into a v2 one.

func (f titleField) calibreSort() *namedMetaSlot { return f.o.namedMeta("calibre:title_sort") }

// get returns the title and its sort value, preferring the standard refinement
// and falling back to the calibre meta, so a v2 file written by calibre reads
// back the sort title it carries.
func (f titleField) get() (title, sort string) {
	el := f.element()
	sort = el.refine("file-as").get()
	if sort == "" {
		sort = f.calibreSort().get()
	}
	return el.get(), sort
}

func (f titleField) set(title, sort *string) {
	if title == nil && sort == nil {
		return
	}

	el := f.element()
	if title != nil {
		el.set(*title)
		f.dropSegments(el.ensure())
	}

	// A title written without one drops the sort title it used to carry.
	value := ""
	if sort != nil {
		value = xml.Collapse(*sort)
	}

	// Rewritten in place wherever the file has a refinement, whatever version it
	// claims, since a stale one would outrank the calibre meta on the way back
	// in. A v3 package with none gets one; a v2 package with none stays without.
	refine := el.refine("file-as")
	if refine.exists() || f.o.epub3() {
		put(refine, value)
	}

	// A v2 package always gets the calibre meta; a v3 package only if it already
	// carried one, kept in step rather than left contradicting the refinement.
	if f.o.epub3() && !f.calibreSort().exists() {
		return
	}
	put(f.calibreSort(), value)
}

// dropSegments removes every dc:title except keep, with its refinements, so a
// replaced title leaves the document recording exactly the one it now has.
//
// A further dc:title is another segment of the same title — §5.5.3.1.2's
// multipart example is "THE LORD OF THE RINGS" followed by "Part One: The
// Fellowship of the Ring" — rather than a separate field. Once the title has
// been replaced they describe a title the book no longer has, and §5.5.3.1.2
// asks for a single element regardless: "EPUB creators should use only a
// single dc:title element to ensure consistent rendering of the title in
// reading systems."
//
// It is also what stops an edit from silently not taking. A reading system
// honouring the deprecated title-type refinement, as calibre does, shows the
// segment labelled "main" — which need not be the element §5.5.3.1.2 makes
// ours to write. Leaving the others would rename the book everywhere except
// there.
//
// keep's own refinements stay, including a title-type that may now be the only
// one left and read oddly. Harmless: with one element left, first in document
// order and "the one labelled main" resolve to it either way.
func (f titleField) dropSegments(keep *etree.Element) {
	var ids []string
	for _, el := range f.o.elements("title") {
		if el == keep {
			continue
		}
		ids = append(ids, attr(el, "id"))
		detach(el)
	}
	f.o.removeRefinements(ids)
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
//
// §5.5.3.2.4 forbids more than one dc:date, so coping with several is tolerance
// for a malformed file rather than a case the spec allows.
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
