package opf

import (
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/library/internal/epub/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/xml"
)

// The fields whose whole encoding fits in a few lines. The three with more to
// say — authors, series, cover — keep a file each.

type titleField struct{ d *pkgdoc.Doc }

func (o *Doc) title() titleField { return titleField{o.d} }

func (f titleField) element() *pkgdoc.Element { return f.d.DC("title") }

// The sort title has the same shape as the series: a standard EPUB 3 mechanism,
// and for EPUB 2, which has none, the proprietary meta calibre writes. Checked
// against calibre itself — `ebook-meta --title-sort` writes a file-as refinement
// into a v3 package and calibre:title_sort into a v2 one.

func (f titleField) calibreSort() *pkgdoc.Named { return f.d.Named("calibre:title_sort") }

// get returns the title and its sort value, preferring the standard refinement
// and falling back to the calibre meta, so a v2 file written by calibre reads
// back the sort title it carries.
func (f titleField) get() (title, sort string) {
	el := f.element()
	sort = el.Refine("file-as").Get()
	if sort == "" {
		sort = f.calibreSort().Get()
	}
	return el.Get(), sort
}

func (f titleField) set(title, sort *string) {
	if title == nil && sort == nil {
		return
	}

	el := f.element()
	if title != nil {
		el.Set(*title)
		f.dropSegments(el)
	}

	// A title written without one drops the sort title it used to carry.
	value := ""
	if sort != nil {
		value = xml.Collapse(*sort)
	}

	// Rewritten in place wherever the file has a refinement, whatever version it
	// claims, since a stale one would outrank the calibre meta on the way back
	// in. A v3 package with none gets one; a v2 package with none stays without.
	refine := el.Refine("file-as")
	if refine.Exists() || f.d.EPUB3() {
		pkgdoc.Put(refine, value)
	}

	// A v2 package always gets the calibre meta; a v3 package only if it already
	// carried one, kept in step rather than left contradicting the refinement.
	if f.d.EPUB3() && !f.calibreSort().Exists() {
		return
	}
	pkgdoc.Put(f.calibreSort(), value)
}

// dropSegments removes every dc:title except keep, with its refinements. A
// further dc:title is another segment of the same title (§5.5.3.1.2's multipart
// example), so once the title is replaced they describe one the book no longer
// has; §5.5.3.1.2 asks for "only a single dc:title element" regardless.
//
// It also stops the edit silently not taking: a reader honouring the deprecated
// title-type refinement, as calibre does, shows the segment labelled "main",
// which need not be the element we write.
//
// keep's own refinements stay. A title-type left alone on the last element is
// harmless — both readings resolve to it.
func (f titleField) dropSegments(keep *pkgdoc.Element) {
	for _, el := range f.d.DCAll("title") {
		if !el.Same(keep) {
			el.Remove()
		}
	}
}

type modifiedField struct{ d *pkgdoc.Doc }

func (o *Doc) modified() modifiedField { return modifiedField{o.d} }

// set records the time of this rewrite: exactly one unrefined dcterms:modified
// per §5.5.5, in the UTC format §5.5.4 fixes. Write-only, and EPUB 3 only.
func (f modifiedField) set(t time.Time) {
	if !f.d.EPUB3() {
		return
	}
	f.d.UnrefinedMeta("dcterms:modified", "").Set(t.UTC().Format("2006-01-02T15:04:05Z"))
}

// description and language are repeatable (§5.5.3.2.1) but single-valued to us,
// and have no encoding of their own, so they get no field type: pkgdoc's DC picks
// the element a read and a write both mean.
func (o *Doc) description() string { return o.d.DC("description").Get() }

func (o *Doc) language() string { return o.d.DC("language").Get() }

// identifiers keys by the element's XML id, which is the wrong key and frozen
// that way: changing it is a schema migration, and
// TestIdentifiersKeepTheirCurrentKeying guards it. Read-only.
func (o *Doc) identifiers() map[string]string {
	out := map[string]string{}
	for _, el := range o.d.DCAll("identifier") {
		out[el.ID()] = el.Get()
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
	for _, d := range o.d.DCAll("date") {
		val := d.Get()
		if val == "" {
			continue
		}
		event := d.OPFAttr("event").Get()
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
