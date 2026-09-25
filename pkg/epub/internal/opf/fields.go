package opf

import (
	"strings"
	"time"

	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/opf/pkgdoc"
	"github.com/ramblingenzyme/ebookfs/pkg/epub/internal/xml"
)

type titleField struct{ d *pkgdoc.Doc }

func (o *Doc) title() titleField { return titleField{o.d} }

func (f titleField) element() *pkgdoc.Element { return f.d.DC("title") }

// calibreSort is where an EPUB 2 package keeps its sort title. Checked against
// calibre: `ebook-meta --title-sort` writes a file-as refinement into a v3
// package and calibre:title_sort into a v2 one.
func (f titleField) calibreSort() *pkgdoc.Named { return f.d.Named("calibre:title_sort") }

// get prefers the file-as refinement for the sort title and falls back to
// calibre's meta.
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

	value := ""
	if sort != nil {
		value = xml.Collapse(*sort)
	}

	refine := el.Refine("file-as")
	if writeV3(f.d, refine.Exists()) {
		pkgdoc.Put(refine, value)
	}
	if writeCalibre(f.d, f.calibreSort().Exists()) {
		pkgdoc.Put(f.calibreSort(), value)
	}
}

// dropSegments removes every dc:title except keep, with its refinements. The
// others are segments of the old title (§5.5.3.1.2's multipart example), and
// §5.5.3.1.2 asks for "only a single dc:title element".
//
// It also makes the edit visible to calibre, which shows the segment labelled
// "main" by the deprecated title-type refinement.
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
// per §5.5.5, in the UTC format §5.5.4 fixes. EPUB 3 only.
func (f modifiedField) set(t time.Time) {
	if !f.d.EPUB3() {
		return
	}
	f.d.UnrefinedMeta("dcterms:modified").Set(t.UTC().Format("2006-01-02T15:04:05Z"))
}

// description and language are repeatable (§5.5.3.2.1) but single-valued
// here. pkgdoc's DC picks the element a read and a write both mean, so they
// need no field type.
func (o *Doc) description() string { return o.d.DC("description").Get() }

func (o *Doc) language() string { return o.d.DC("language").Get() }

// pubdate returns a dc:date as written. An opf:event of "publication" wins.
// Otherwise a date with any other event is not the publication date, and the
// one untagged date is used; zero or several leave it unset.
//
// §5.5.3.2.4 forbids more than one dc:date, so several is a malformed file.
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
