package epub_test

import (
	"bytes"
	"errors"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"slices"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

type corpus struct {
	name    string
	opf     epubtest.PackageDoc
	foreign []item
	// sortTitle is false for a fixture with no sort title to preserve.
	sortTitle bool
}

func corpora() []corpus {
	return []corpus{
		{
			name:      "epub3",
			opf:       epubtest.RichOPF3,
			sortTitle: true,
			foreign: []item{
				{"author alternate-script", "//meta[@property='alternate-script']", "ドゥ・ジェーン"},
				{"series identifier", "//meta[@property='dcterms:identifier']", "urn:issn:1234-5678"},
				{"set collection", "//meta[@id='set1']", "Complete Works"},
				{"set collection-type", "//meta[@refines='#set1']", "set"},
				{"editor", "//contributor[@id='ed1']", "An Editor"},
				{"book identifier", "//identifier[@id='bookid']", "urn:uuid:1234"},
				{"publication date", "//date", "2020-01-02"},
				{"calibre column", "//meta[@name='calibre:user_rating']", ""},
				// Presence only: ebookfs updates this one on every rewrite, and
				// TestSpecModifiedIsUpdated owns its value.
				{"modified timestamp", "//meta[@property='dcterms:modified']", ""},
			},
		},
		{
			name: "epub2",
			opf:  epubtest.RichOPF2,
			foreign: []item{
				{"editor", "//contributor[@id='ed1']", "An Editor"},
				{"book identifier", "//identifier[@id='bookid']", "urn:uuid:1234"},
				{"publication date", "//date", "2020-01-02"},
				{"calibre column", "//meta[@name='calibre:user_rating']", ""},
				{"publisher column", "//meta[@name='publisher:internal-id']", ""},
			},
		},
	}
}

// Clearing the series and renaming an author are left out, since they are
// meant to take their refinements with them.
func preservingEdits() []struct {
	name string
	e    func(*epub.Book)
} {
	authors := []epub.Author{{Name: "Jane Doe", SortName: "Doe, Jane"}}
	return []struct {
		name string
		e    func(*epub.Book)
	}{
		{"title", func(b *epub.Book) { b.Title = "New Title" }},
		{"sort title", func(b *epub.Book) { b.SortTitle = "Title, New" }},
		{"description", func(b *epub.Book) { b.Description = "A new description." }},
		{"language", func(b *epub.Book) { b.Language = "fr" }},
		{"authors unchanged", func(b *epub.Book) { b.Authors = authors }},
		{"series rename", func(b *epub.Book) { rename(b, "The Quartet") }},
		{"series index", func(b *epub.Book) { reposition(b, "4") }},
	}
}

func TestSavePreservesForeignMetadata(t *testing.T) {
	for _, c := range corpora() {
		for _, tc := range preservingEdits() {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				path := epubtest.Build(t, c.opf)
				if _, err := save(t, path, tc.e); err != nil {
					t.Fatal(err)
				}
				opf := epubtest.ReadEntry(t, path, epubtest.OPFPath)

				doc := etree.NewDocument()
				if err := doc.ReadFromBytes(opf); err != nil {
					t.Fatalf("result is not parseable XML: %v", err)
				}
				md := doc.FindElement("//metadata")
				if md == nil {
					t.Fatal("result has no <metadata>")
				}
				for _, f := range c.foreign {
					el := md.FindElement(f.path)
					if el == nil {
						t.Errorf("%s (%s) was removed", f.name, f.path)
						continue
					}
					if f.text != "" && el.Text() != f.text {
						t.Errorf("%s = %q, want %q", f.name, el.Text(), f.text)
					}
				}
				if !bytes.Contains(opf, []byte("<!-- a comment nobody should eat -->")) {
					t.Error("the XML comment was removed")
				}
				assertOutsideMetadataUnchanged(t, c.opf, opf)
			})
		}
	}
}

// Comparing the serialized manifest and spine catches a dropped attribute or a
// reordered item without a list to keep in step with the fixtures. A cover
// edit is meant to touch the manifest, so this is for metadata edits only.
func assertOutsideMetadataUnchanged(t *testing.T, before epubtest.PackageDoc, after []byte) {
	t.Helper()

	serialize := func(src []byte, tag string) string {
		doc := etree.NewDocument()
		if err := doc.ReadFromBytes(src); err != nil {
			t.Fatalf("not parseable XML: %v", err)
		}
		el := doc.FindElement("//" + tag)
		if el == nil {
			t.Fatalf("no <%s>", tag)
		}
		d := etree.NewDocument()
		d.SetRoot(el.Copy())
		out, err := d.WriteToString()
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	for _, tag := range []string{"manifest", "spine"} {
		want := serialize([]byte(before), tag)
		if got := serialize(after, tag); got != want {
			t.Errorf("<%s> changed by a metadata-only edit:\n before: %s\n  after: %s", tag, want, got)
		}
	}
}

// Clearing the sort title on a retitle is the library's rule, not this package's.
func TestTitleEditLeavesTheSortTitle(t *testing.T) {
	for _, c := range corpora() {
		if !c.sortTitle {
			continue
		}
		t.Run(c.name, func(t *testing.T) {
			path := epubtest.Build(t, c.opf)
			was := open(t, path).SortTitle
			if was == "" {
				t.Fatal("fixture carries no sort title, so this proves nothing")
			}
			b, err := save(t, path, func(b *epub.Book) { b.Title = "New Title" })
			if err != nil {
				t.Fatal(err)
			}
			if b.SortTitle != was {
				t.Errorf("sort title = %q, want %q unchanged by the title edit", b.SortTitle, was)
			}
		})
	}
}

func TestSaveRoundTrips(t *testing.T) {
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}, {Name: "Bo Li"}}

	cases := []struct {
		name  string
		e     func(*epub.Book)
		check func(*testing.T, *epub.Book)
	}{
		{"title", func(b *epub.Book) { b.Title = "New Title" }, func(t *testing.T, b *epub.Book) {
			if b.Title != "New Title" {
				t.Errorf("title = %q", b.Title)
			}
		}},
		{"sort title", func(b *epub.Book) { b.SortTitle = "Title, New" }, func(t *testing.T, b *epub.Book) {
			if b.SortTitle != "Title, New" {
				t.Errorf("sort title = %q", b.SortTitle)
			}
		}},
		{"description", func(b *epub.Book) { b.Description = "A new description." }, func(t *testing.T, b *epub.Book) {
			if b.Description != "A new description." {
				t.Errorf("description = %q", b.Description)
			}
		}},
		{"language", func(b *epub.Book) { b.Language = "fr" }, func(t *testing.T, b *epub.Book) {
			if b.Language != "fr" {
				t.Errorf("language = %q", b.Language)
			}
		}},
		{"authors", func(b *epub.Book) { b.Authors = authors }, func(t *testing.T, b *epub.Book) {
			if len(b.Authors) != 2 || b.Authors[0].Name != "Ann Rand" || b.Authors[1].Name != "Bo Li" {
				t.Fatalf("authors = %+v", b.Authors)
			}
			if b.Authors[0].SortName != "Rand, Ann" || b.Authors[1].SortName != "" {
				t.Errorf("sort names = %q, %q", b.Authors[0].SortName, b.Authors[1].SortName)
			}
		}},
		{"series rename keeps position", func(b *epub.Book) { rename(b, "The Quartet") }, func(t *testing.T, b *epub.Book) {
			if b.Series == nil || b.Series.Name != "The Quartet" || b.Series.Index != "2" {
				t.Errorf("series = %+v, want The Quartet at 2", b.Series)
			}
		}},
		{"series index keeps name", func(b *epub.Book) { reposition(b, "4") }, func(t *testing.T, b *epub.Book) {
			if b.Series == nil || b.Series.Name != "The Trilogy" || b.Series.Index != "4" {
				t.Errorf("series = %+v, want The Trilogy at 4", b.Series)
			}
		}},
		{"series cleared", func(b *epub.Book) { rename(b, "") }, func(t *testing.T, b *epub.Book) {
			if b.Series != nil {
				t.Errorf("series = %+v, want nil", b.Series)
			}
		}},
	}

	for _, c := range corpora() {
		for _, tc := range cases {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				path := epubtest.Build(t, c.opf)
				bib, err := save(t, path, tc.e)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, bib)

				// Save leaves the Book reading the rewritten file, so a fresh
				// Open checks the write reached the disk.
				fresh, err := parse(t, path)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, fresh)
			})
		}
	}
}

// A second identical write must produce the same bytes, or repeated edits
// grow the file. synctest freezes the clock so dcterms:modified cannot differ.
func TestSaveIsIdempotent(t *testing.T) {
	for _, c := range corpora() {
		for _, tc := range preservingEdits() {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					path := epubtest.Build(t, c.opf)
					if _, err := save(t, path, tc.e); err != nil {
						t.Fatal(err)
					}
					once := epubtest.ReadEntry(t, path, epubtest.OPFPath)

					if _, err := save(t, path, tc.e); err != nil {
						t.Fatal(err)
					}
					twice := epubtest.ReadEntry(t, path, epubtest.OPFPath)

					if !bytes.Equal(once, twice) {
						t.Errorf("second identical edit changed the file\n--- after one ---\n%s\n--- after two ---\n%s", once, twice)
					}
				})
			})
		}
	}
}

// The first edit declares a dcterms2 prefix. A later edit that did not
// recognise it would declare dcterms3, dcterms4, one more per save.
func TestSaveIsIdempotentOnARebindingDocument(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <meta property="dcterms:modified">not-a-date</meta>`, Attrs: `prefix="dcterms: http://example.com/vocab#"`}.EPUB3()

	path := epubtest.Build(t, opf)
	var first string
	for i, title := range []string{"One", "Two", "Three"} {
		if _, err := save(t, path, func(b *epub.Book) { b.Title = title }); err != nil {
			t.Fatalf("edit %d: %v", i+1, err)
		}

		md := epubtest.Metadata(t, path)
		declared := md.Parent().SelectAttrValue("prefix", "")
		if i == 0 {
			first = declared
		} else if declared != first {
			t.Fatalf("edit %d grew the prefix attribute:\n first: %s\n  now: %s", i+1, first, declared)
		}

		var modified int
		for _, m := range md.SelectElements("meta") {
			if strings.HasSuffix(m.SelectAttrValue("property", ""), ":modified") {
				modified++
			}
		}
		if modified != 2 {
			t.Fatalf("edit %d left %d modified properties, want 2", i+1, modified)
		}
		var theirs *etree.Element
		for _, m := range md.SelectElements("meta") {
			if m.SelectAttrValue("property", "") == "dcterms:modified" {
				theirs = m
			}
		}
		if theirs == nil || theirs.Text() != "not-a-date" {
			t.Fatalf("edit %d touched the document's own dcterms:modified: %v", i+1, theirs)
		}
	}
}

// OPF 2.0 §2.2.10 lets an EPUB 2 package carry a belongs-to-collection meta, so
// a file can hold the series in two encodings that disagree.
func TestEPUB2SeriesEditUpdatesBothEncodings(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB2(epubtest.Metas(
		`<dc:creator opf:role="aut">Ann Rand</dc:creator>`,
		epubtest.Collection("c01", "The Old Series", "series", "2"),
	)))

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Old Series" {
		t.Fatalf("series before the edit = %+v, want The Old Series", bib.Series)
	}

	want := "The New Series"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}
	assertSeriesEncodings(t, path, want, "2")
}

// calibre reads its own metas first, so they are kept in step.
func TestEPUB3SeriesEditUpdatesStaleCalibreMetas(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(epubtest.Metas(
		epubtest.Collection("c01", "The Old Series", "series", "2"),
		epubtest.CalibreSeries("The Old Series", "2"),
	)))

	want := "The New Series"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}
	assertSeriesEncodings(t, path, want, "2")
}

func TestEPUB3SeriesEditDoesNotInjectCalibreMetas(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(epubtest.Metas(epubtest.Collection("c01", "The Old Series", "series", ""))))

	want := "The New Series"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}
	if got := epubtest.Metadata(t, path).FindElement("//meta[@name='calibre:series']"); got != nil {
		t.Errorf("calibre:series = %q, want none — the file never carried that encoding",
			got.SelectAttrValue("content", ""))
	}
}

func assertSeriesEncodings(t *testing.T, path, want, wantIndex string) {
	t.Helper()
	md := epubtest.Metadata(t, path)

	var collections []string
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" {
			collections = append(collections, m.Text())
		}
	}
	if !slices.Equal(collections, []string{want}) {
		t.Errorf("collections = %v, want exactly [%s]", collections, want)
	}
	if got := epubtest.Property(t, md, "//meta[@property='group-position']"); got != wantIndex {
		t.Errorf("group-position = %q, want %q carried over", got, wantIndex)
	}
	if got := epubtest.LegacyMeta(t, md, "//meta[@name='calibre:series']"); got != want {
		t.Errorf("calibre:series = %q, want %q in step with the collection", got, want)
	}
	if got := epubtest.LegacyMeta(t, md, "//meta[@name='calibre:series_index']"); got != wantIndex {
		t.Errorf("calibre:series_index = %q, want %q", got, wantIndex)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != want || bib.Series.Index != wantIndex {
		t.Errorf("series = %+v, want %q at %s", bib.Series, want, wantIndex)
	}
}

// calibre:series_index is a float by calibre's convention, so only two levels fit.
func TestMultiLevelPositionNarrowsForEPUB2(t *testing.T) {
	opf := epubtest.EPUB2(epubtest.Metas(
		`<dc:title>An Article</dc:title>`,
		`<dc:creator opf:role="aut">Ann Rand</dc:creator>`,
		epubtest.CalibreSeries("Physical Review D", "1"),
	))

	path := epubtest.Build(t, opf)
	index := "2.2.1"
	if _, err := save(t, path, func(b *epub.Book) { reposition(b, index) }); err != nil {
		t.Fatal(err)
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "2.2" {
		t.Errorf("series = %+v, want the position narrowed to 2.2 by the EPUB 2 encoding", bib.Series)
	}
}

// The collection has no position, so a stale calibre:series_index would contradict it.
func TestSeriesWithNoPositionDropsTheCalibreIndex(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB2(epubtest.Metas(
		`<dc:creator opf:role="aut">Ann Rand</dc:creator>`,
		epubtest.Collection("c01", "The Old Series", "series", ""),
		epubtest.CalibreSeries("The Old Series", "7"),
	)))

	want := "The New Series"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
	if got := epubtest.LegacyMeta(t, md, "meta[@name='calibre:series']"); got != want {
		t.Errorf("calibre:series = %q, want %q", got, want)
	}
	if el := md.FindElement("meta[@name='calibre:series_index']"); el != nil {
		t.Errorf("calibre:series_index = %q, want it gone with the position", el.SelectAttrValue("content", ""))
	}
}

// The creator element is reused, so its opf:file-as must be removed.
func TestEPUB2CreatorLosesAStaleSortName(t *testing.T) {
	opf := epubtest.EPUB2(`    <dc:creator opf:role="aut" opf:file-as="Doe, Jane">Jane Doe</dc:creator>`)

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Jane Doe"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	if got := epubtest.AttrOf(t, epubtest.Metadata(t, path), "creator", "opf:file-as"); got != "" {
		t.Errorf("opf:file-as = %q, want removed with the sort name", got)
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "" {
		t.Errorf("authors = %+v, want one author with no sort name", bib.Authors)
	}
}

// Common in EPUB 2 files upgraded in place. The read prefers the attribute, so
// an edit that only wrote the refinement would never be read back.
func TestEPUB3CreatorWithALegacySortNameTakesTheEdit(t *testing.T) {
	opf := epubtest.PackageDoc(strings.Replace(string(epubtest.EPUB3(`    <dc:creator id="c1" opf:file-as="Stale, Name">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>`)),
		`xmlns:dc="http://purl.org/dc/elements/1.1/"`,
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf"`, 1))

	path := saveSortName(t, opf)
	c := epubtest.Metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "" && got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want it updated or removed, not left stale", got)
	}
}

// The read matches file-as by local name. A write that always added opf:file-as
// would leave two sort names, and the next read would take the stale one.
func TestUnprefixedFileAsIsUpdatedNotDuplicated(t *testing.T) {
	opf := epubtest.EPUB2(`    <dc:creator opf:role="aut" file-as="Stale, Name">Ann Rand</dc:creator>`)

	path := saveSortName(t, opf)

	c := epubtest.Metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	var fileAs []string
	for _, a := range c.Attr {
		if a.Key == "file-as" {
			fileAs = append(fileAs, a.FullKey()+"="+a.Value)
		}
	}
	if len(fileAs) != 1 {
		t.Errorf("file-as attributes = %v, want exactly one", fileAs)
	}
}

// §5.5.2 makes an empty value invalid and says no more. Writing into the empty
// element, rather than adding one, is our choice.
func TestEmptyElementIsWrittenInPlace(t *testing.T) {
	opf := epubtest.EPUB3(`    <dc:description>   </dc:description>
    <dc:description></dc:description>`)

	path := epubtest.Build(t, opf)
	desc := "A new description."
	if _, err := save(t, path, func(b *epub.Book) { b.Description = desc }); err != nil {
		t.Fatal(err)
	}

	els := epubtest.Metadata(t, path).SelectElements("description")
	if len(els) != 2 {
		t.Fatalf("description count = %d, want the two the file had", len(els))
	}
	if els[0].Text() != desc {
		t.Errorf("first description = %q, want the edit", els[0].Text())
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != desc {
		t.Errorf("description = %q, want the edit read back", bib.Description)
	}
}

// ponytail: D.3.6 gives file-as "Cardinality: zero or one". The write updates
// the first of two and leaves the second, which the read ignores. Delete this
// test if a file turns up where it matters.
func TestDuplicateRefinementsKeepTheirDuplicates(t *testing.T) {
	opf := epubtest.EPUB3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="file-as">First, Ann</meta>
    <meta refines="#c1" property="file-as">Second, Ann</meta>`)

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, m := range epubtest.Metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "file-as" {
			got = append(got, m.Text())
		}
	}
	if !slices.Equal(got, []string{"Rand, Ann", "Second, Ann"}) {
		t.Errorf("file-as refines = %v, want the first updated and the second left", got)
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("sort name = %q, want the refinement the write updated", bib.Authors[0].SortName)
	}
}

// Nothing in §5.3.1 or §5.3.7 requires this. It holds because elements are
// reused rather than rebuilt.
func TestMetadataDirectionalitySurvives(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <dc:title xml:lang="ar" dir="rtl">العنوان</dc:title>`, Attrs: `xml:lang="en" dir="ltr"`}.EPUB3()

	path := epubtest.Build(t, opf)
	desc := "A new description."
	if _, err := save(t, path, func(b *epub.Book) { b.Description = desc }); err != nil {
		t.Fatal(err)
	}
	title := epubtest.Metadata(t, path).SelectElement("title")
	if title == nil {
		t.Fatal("title removed")
	}
	if got := title.SelectAttrValue("dir", ""); got != "rtl" {
		t.Errorf("dir = %q, want rtl", got)
	}
	if got := title.SelectAttrValue("lang", ""); got != "ar" {
		t.Errorf("xml:lang = %q, want ar", got)
	}
}

func TestUnmodelledMetadataSurvives(t *testing.T) {
	var opf = epubtest.EPUB3(`    <dc:subject>Science Fiction</dc:subject>
    <dc:subject>Space Opera</dc:subject>
    <dc:publisher>Acme Press</dc:publisher>
    <dc:rights>All rights reserved.</dc:rights>
    <dc:source>urn:isbn:9780000000000</dc:source>`)

	path := epubtest.Build(t, opf)
	want := "A New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}
	md := epubtest.Metadata(t, path)
	for _, tag := range []string{"publisher", "rights", "source"} {
		if el := md.SelectElement(tag); el == nil {
			t.Errorf("dc:%s was dropped", tag)
		}
	}
	if n := len(md.SelectElements("subject")); n != 2 {
		t.Errorf("dc:subject count = %d, want 2", n)
	}
}

// D.3.4 defines series and set only "when no scheme is specified".
func TestSchemedCollectionTypeSurvivesASeriesEdit(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(`    <meta property="belongs-to-collection" id="c01">The Trilogy</meta>
    <meta refines="#c01" property="collection-type" scheme="onix:codelist148">12</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">2</meta>`))

	want := "The Quartet"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}

	var schemed, unschemed []string
	for _, m := range epubtest.Metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") != "collection-type" {
			continue
		}
		if m.SelectAttrValue("scheme", "") != "" {
			schemed = append(schemed, m.Text())
		} else {
			unschemed = append(unschemed, m.Text())
		}
	}
	if !slices.Equal(schemed, []string{"12"}) {
		t.Errorf("schemed collection-type = %v, want [12] untouched — it is drawn from onix:codelist148, not ours", schemed)
	}
	if !slices.Equal(unschemed, []string{"series"}) {
		t.Errorf("unschemed collection-type = %v, want [series]", unschemed)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != want || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want %q at 2", bib.Series, want)
	}
}

// A collection the reader cannot resolve is invisible to the writer too, so a
// rename would add a second one without a position.
func TestSeriesRenameDoesNotDuplicate(t *testing.T) {

	path := epubtest.Build(t, opfSpecStyleWhitespace)
	want := "Renamed"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}
	md := epubtest.Metadata(t, path)
	var collections int
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" {
			collections++
		}
	}
	if collections != 1 {
		t.Errorf("belongs-to-collection count = %d, want 1 — the rename duplicated it", collections)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want the position preserved across a rename", bib.Series)
	}
}

func ncxWith(body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head><meta name="dtb:uid" content="urn:uuid:1234"/></head>
` + body + `
  <navMap><navPoint id="p1" playOrder="1"><navLabel><text>Chapter One</text></navLabel><content src="chapter1.xhtml"/></navPoint></navMap>
</ncx>`
}

func buildNCXEpub(t *testing.T, ncx string) string {
	t.Helper()
	return epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.NCXOPF, epubtest.Entry{Name: "OEBPS/toc.ncx", Data: []byte(ncx)}))
}

func ncxTexts(t *testing.T, epubPath, tag string) []string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(epubtest.ReadEntry(t, epubPath, "OEBPS/toc.ncx")); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	var out []string
	for _, el := range doc.FindElements("//" + tag) {
		out = append(out, strings.TrimSpace(el.SelectElement("text").Text()))
	}
	return out
}

var ncxWithTitle = ncxWith(`  <docTitle><text>Original Title</text></docTitle>`)

func TestNCXDocTitleFollowsATitleEdit(t *testing.T) {
	path := buildNCXEpub(t, ncxWithTitle)

	title := "New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = title }); err != nil {
		t.Fatal(err)
	}

	if got := ncxTexts(t, path, "docTitle"); len(got) != 1 || got[0] != title {
		t.Errorf("docTitle = %q, want [%q]", got, title)
	}
}

func TestNCXDocAuthorsAreReconciled(t *testing.T) {
	path := buildNCXEpub(t, ncxWith(`  <docTitle><text>Original Title</text></docTitle>
  <docAuthor><text>Jane Doe</text></docAuthor>
  <docAuthor><text>Dropped Coauthor</text></docAuthor>`))

	authors := []epub.Author{{Name: "Ann Rewrite"}, {Name: "Bo Second"}, {Name: "Cy Third"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	want := []string{"Ann Rewrite", "Bo Second", "Cy Third"}
	if got := ncxTexts(t, path, "docAuthor"); !slices.Equal(got, want) {
		t.Errorf("docAuthor = %q, want %q", got, want)
	}

	// The NCX content model puts <navMap> after docAuthor, so appending to
	// <ncx> would make the file invalid.
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(epubtest.ReadEntry(t, path, "OEBPS/toc.ncx")); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, el := range doc.SelectElement("ncx").ChildElements() {
		tags = append(tags, el.Tag)
	}
	wantTags := []string{"head", "docTitle", "docAuthor", "docAuthor", "docAuthor", "navMap"}
	if !slices.Equal(tags, wantTags) {
		t.Errorf("ncx children = %q, want %q", tags, wantTags)
	}
}

func TestNCXWithNoDocAuthorGainsNone(t *testing.T) {
	path := buildNCXEpub(t, ncxWithTitle)

	authors := []epub.Author{{Name: "Ann Rewrite"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	if got := ncxTexts(t, path, "docAuthor"); len(got) != 0 {
		t.Errorf("docAuthor = %q, want none", got)
	}
}

func TestTitleEditWithoutAnNCX(t *testing.T) {
	path := epubtest.Build(t, epubtest.OPF3)

	title := "New Title"
	bib, err := save(t, path, func(b *epub.Book) { b.Title = title })
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != title {
		t.Errorf("title = %q, want %q", bib.Title, title)
	}
}

func TestNCXUntouchedByAnUnrelatedEdit(t *testing.T) {
	ncx := ncxWithTitle
	path := buildNCXEpub(t, ncx)

	series := "A Series"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, series) }); err != nil {
		t.Fatal(err)
	}

	if got := string(epubtest.ReadEntry(t, path, "OEBPS/toc.ncx")); got != ncx {
		t.Errorf("ncx was rewritten:\n%s", got)
	}
}

// The second case is malformed only in its nesting, which etree would otherwise correct silently.
func TestUnreadableNCXDoesNotFailTheEdit(t *testing.T) {
	for _, tc := range []struct{ name, ncx string }{
		{"syntax error", "<ncx><docTitle<</ncx>"},
		{"mismatched end tag", `<ncx><docTitle><text>Original Title</text></docTitle>` +
			`<navMap><navPoint id="p1"><content src="chapter1.xhtml"/></navPoint></wrong></ncx>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildNCXEpub(t, tc.ncx)

			title := "New Title"
			bib, err := save(t, path, func(b *epub.Book) { b.Title = title })
			if err != nil {
				t.Fatal(err)
			}
			if bib.Title != title {
				t.Errorf("title = %q, want %q", bib.Title, title)
			}
			if got := string(epubtest.ReadEntry(t, path, "OEBPS/toc.ncx")); got != tc.ncx {
				t.Errorf("unreadable ncx was rewritten:\n%s", got)
			}
		})
	}
}

func TestCDataDescriptionKeepsItsSpelling(t *testing.T) {
	// Inside CDATA, &amp; is five literal characters, so this is also the value
	// the reader must report.
	const value = `<p>Fancy &amp; <b>bold</b></p>`
	path := epubtest.Build(t, epubtest.EPUB3(`    <dc:creator>Jane Doe</dc:creator>
    <dc:description><![CDATA[`+value+`]]></dc:description>`))

	title := "New Title"
	bib, err := save(t, path, func(b *epub.Book) { b.Title = title })
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != value {
		t.Errorf("description = %q, want %q", bib.Description, value)
	}

	got := string(epubtest.ReadEntry(t, path, epubtest.OPFPath))
	if !strings.Contains(got, "<![CDATA["+value+"]]>") {
		t.Errorf("the CDATA section was re-encoded:\n%s", got)
	}
}

// §5.5.3.1.2's own example: the second element is another segment of the same title.
var opfMultipartTitle = epubtest.EPUB3(`    <dc:title>THE LORD OF THE RINGS</dc:title>
    <dc:title>Part One: The Fellowship of the Ring</dc:title>`)

func TestTitleEditTakesTheOtherSegments(t *testing.T) {
	path := epubtest.Build(t, opfMultipartTitle)

	want := "The Hobbit"
	bib, err := save(t, path, func(b *epub.Book) { b.Title = want })
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != want {
		t.Errorf("title = %q, want %q", bib.Title, want)
	}

	els := epubtest.Metadata(t, path).SelectElements("title")
	if len(els) != 1 {
		t.Fatalf("dc:title count = %d, want 1", len(els))
	}
	if got := strings.TrimSpace(els[0].Text()); got != want {
		t.Errorf("dc:title = %q, want %q", got, want)
	}
}

func TestSortTitleEditLeavesTheOtherSegments(t *testing.T) {
	path := epubtest.Build(t, opfMultipartTitle)

	sort := "Lord of the Rings, The"
	if _, err := save(t, path, func(b *epub.Book) { b.SortTitle = sort }); err != nil {
		t.Fatal(err)
	}

	if els := epubtest.Metadata(t, path).SelectElements("title"); len(els) != 2 {
		t.Errorf("dc:title count = %d, want the two the file had", len(els))
	}
}

func TestTitleEditIsIdempotentAcrossSegments(t *testing.T) {
	path := epubtest.Build(t, opfMultipartTitle)

	want := "The Hobbit"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}
	first := epubtest.ReadEntry(t, path, epubtest.OPFPath)

	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}
	if second := epubtest.ReadEntry(t, path, epubtest.OPFPath); !bytes.Equal(first, second) {
		t.Errorf("a repeated edit rewrote the package document:\n%s", second)
	}
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSaveWritesTheSimpleFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{{"epub3", epubtest.OPF3}, {"epub2", epubtest.OPF2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(tc.opf))
			book, err := save(t, path, func(b *epub.Book) { b.Title = "New Title"; b.Description = "New description."; b.Language = "fr" })
			if err != nil {
				t.Fatal(err)
			}
			if book.Title != "New Title" {
				t.Errorf("title = %q, want New Title", book.Title)
			}
			if book.Description != "New description." {
				t.Errorf("description = %q", book.Description)
			}
			if book.Language != "fr" {
				t.Errorf("language = %q, want fr", book.Language)
			}
			reparsed, err := parse(t, path)
			if err != nil {
				t.Fatal(err)
			}
			if reparsed.Title != "New Title" {
				t.Errorf("persisted title = %q", reparsed.Title)
			}
		})
	}
}

func TestSaveAuthorsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{{"epub3", epubtest.OPF3}, {"epub2", epubtest.OPF2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(tc.opf))
			authors := []epub.Author{
				{Name: "Alice Smith", SortName: "Smith, Alice"},
				{Name: "Bob Jones", SortName: "Jones, Bob"},
			}
			book, err := save(t, path, func(b *epub.Book) { b.Authors = authors })
			if err != nil {
				t.Fatal(err)
			}
			if len(book.Authors) != 2 {
				t.Fatalf("got %d authors, want 2: %+v", len(book.Authors), book.Authors)
			}
			if book.Authors[0].Name != "Alice Smith" || book.Authors[0].SortName != "Smith, Alice" {
				t.Errorf("author[0] = %+v", book.Authors[0])
			}
			if book.Authors[1].Name != "Bob Jones" || book.Authors[1].SortName != "Jones, Bob" {
				t.Errorf("author[1] = %+v", book.Authors[1])
			}
		})
	}
}

func TestSaveSeriesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{{"epub3", epubtest.OPF3}, {"epub2", epubtest.OPF2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(tc.opf))
			book, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Saga", Index: "1.5"} })
			if err != nil {
				t.Fatal(err)
			}
			if book.Series == nil || book.Series.Name != "The Saga" || book.Series.Index != "1.5" {
				t.Errorf("series = %+v, want The Saga at 1.5", book.Series)
			}

			book, err = save(t, path, func(b *epub.Book) { rename(b, "") })
			if err != nil {
				t.Fatal(err)
			}
			if book.Series != nil {
				t.Errorf("series after clear = %+v, want nil", book.Series)
			}
		})
	}
}

func TestSaveWritesTheSortTitle(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	book, err := save(t, path, func(b *epub.Book) { b.Title = "New Title"; b.SortTitle = "New Title, A" })
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "New Title" {
		t.Errorf("title = %q, want New Title", book.Title)
	}
	if book.SortTitle != "New Title, A" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "New Title, A")
	}

	opf, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if !bytes.Contains(opf, []byte(`property="file-as"`)) {
		t.Error("expected a file-as refine for the title sort")
	}
	if bytes.Contains(opf, []byte("calibre:title_sort")) {
		t.Error("OPF must not contain calibre:title_sort")
	}
}

func TestSortTitleEditLeavesTheTitle(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	book, err := save(t, path, func(b *epub.Book) { b.SortTitle = "Sorted, Just" })
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title (unchanged)", book.Title)
	}
	if book.SortTitle != "Sorted, Just" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "Sorted, Just")
	}
}

func TestSortTitleForEPUB2UsesTheCalibreMeta(t *testing.T) {
	// EPUB 2 has no sort-title mechanism, so calibre's meta is used, as for the series.
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF2))
	book, err := save(t, path, func(b *epub.Book) { b.SortTitle = "Sorted, This" })
	if err != nil {
		t.Fatal(err)
	}
	if book.SortTitle != "Sorted, This" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "Sorted, This")
	}

	opf, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if !bytes.Contains(opf, []byte(`<meta name="calibre:title_sort" content="Sorted, This"/>`)) {
		t.Errorf("expected calibre:title_sort, got:\n%s", opf)
	}
	if bytes.Contains(opf, []byte(`property="file-as"`)) {
		t.Error("EPUB 2 OPF must not gain a file-as refine")
	}

	if _, err := save(t, path, func(b *epub.Book) { b.SortTitle = "" }); err != nil {
		t.Fatal(err)
	}
	opf, _ = epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if bytes.Contains(opf, []byte("calibre:title_sort")) {
		t.Errorf("clearing the sort title left a stale calibre:title_sort:\n%s", opf)
	}
}

func TestSaveAcceptsValidLanguageVerbatim(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	// calibre would rewrite "pt-BR" as a three-letter code.
	book, err := save(t, path, func(b *epub.Book) { b.Language = "pt-BR" })
	if err != nil {
		t.Fatal(err)
	}
	if book.Language != "pt-BR" {
		t.Errorf("language = %q, want pt-BR (verbatim, not normalised)", book.Language)
	}
}

func TestSaveRefusesEncryptedOPF(t *testing.T) {
	enc := epubtest.EncryptionXML(epubtest.AES256, "OEBPS/content.opf")
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3, epubtest.Entry{Name: "META-INF/encryption.xml", Data: []byte(enc)}))
	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Hack" }); err == nil {
		t.Fatal("expected refusal on encrypted OPF, got nil")
	}
}

// CipherReference/@URI is a URL, so a space in the entry name is declared as
// %20. Left undecoded, it matches no entry, and the edit rewrites an encrypted package document.
func TestSaveRefusesEncryptedOPFDeclaredAsAURL(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/my%20book.opf", epubtest.PackageMediaType)
	enc := epubtest.EncryptionXML(epubtest.AES256, "OEBPS/my%20book.opf")

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: epubtest.MimetypePath, Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(container)},
		{Name: "META-INF/encryption.xml", Data: []byte(enc)},
		{Name: "OEBPS/my book.opf", Data: []byte(epubtest.OPF3)}, // literal space
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Hack" }); err == nil {
		t.Fatal("edited an encrypted package document declared with a percent-encoded URI")
	}
}

// Some producers write the name unencoded in both places, so the entry's name
// really contains "%20". Matching only the decoded form would treat that
// encrypted entry as readable.
func TestSaveRefusesEncryptedEntryNamedLiterally(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/a%20b.opf", epubtest.PackageMediaType)
	enc := epubtest.EncryptionXML(epubtest.AES256, "OEBPS/a%20b.opf")

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(container)},
		{Name: "META-INF/encryption.xml", Data: []byte(enc)},
		{Name: "OEBPS/a%20b.opf", Data: []byte(epubtest.OPF3)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Hack" }); err == nil {
		t.Fatal("edited an encrypted package document whose URI was written literally")
	}
}

func TestSaveAllowsFontObfuscation(t *testing.T) {
	enc := epubtest.EncryptionXML(epubtest.FontObfusc, "OEBPS/fonts/x.otf")
	entries := epubtest.BaseEntries(epubtest.OPF3,
		epubtest.Entry{Name: "META-INF/encryption.xml", Data: []byte(enc)},
		epubtest.Entry{Name: "OEBPS/fonts/x.otf", Data: []byte("obfuscated-font")},
	)
	path := epubtest.WriteEpub(t, entries)
	book, err := save(t, path, func(b *epub.Book) { b.Title = "Obfuscated OK" })
	if err != nil {
		t.Fatalf("font obfuscation should not block edit: %v", err)
	}
	if book.Title != "Obfuscated OK" {
		t.Errorf("title = %q", book.Title)
	}
}

func TestSetCoverReplacesTheImage(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	swapCover(t, path)
	ch, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/chapter1.xhtml")
	if !ok || !bytes.Equal(ch, epubtest.ChapterBytes) {
		t.Errorf("chapter changed by cover swap")
	}
}

func TestSetCoverRefusesEncrypted(t *testing.T) {
	enc := epubtest.EncryptionXML(epubtest.AES256, "OEBPS/cover.jpg")
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3, epubtest.Entry{Name: "META-INF/encryption.xml", Data: []byte(enc)}))
	if _, err := setCover(t, path, tinyJPEG(t)); err == nil {
		t.Fatal("expected refusal on encrypted cover, got nil")
	}
}

// An SVG entry never becomes the cover, so a GIF is what reaches the format
// rule. The data is a valid GIF, which leaves that rule as the only refusal.
func TestSetCoverRefusesAGIFEntry(t *testing.T) {
	opf := epubtest.Pkg{Manifest: `<item id="cover-img" href="cover.gif" media-type="image/gif" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()
	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(opf)},
		{Name: "OEBPS/cover.gif", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	var img bytes.Buffer
	if err := gif.Encode(&img, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	_, err := setCover(t, path, img.Bytes())
	if err == nil || !strings.Contains(err.Error(), "not replaceable in place") {
		t.Fatalf("err = %v, want the GIF entry refused as not replaceable in place", err)
	}
}

func TestSetCoverRejectsNonImage(t *testing.T) {
	for _, tc := range []struct{ name, data string }{
		{"text", "definitely not an image"},
		{"svg markup", "<svg/>"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
			if _, err := setCover(t, path, []byte(tc.data)); err == nil {
				t.Fatal("expected rejection of non-image cover data, got nil")
			}
		})
	}
}

func TestSetCoverWithNoCoverIsErrNoCover(t *testing.T) {
	path := epubtest.Build(t, epubtest.Pkg{Manifest: epubtest.ChapterOnlyManifest}.EPUB3())
	if _, err := setCover(t, path, tinyJPEG(t)); !errors.Is(err, epub.ErrNoCover) {
		t.Fatalf("err = %v, want ErrNoCover", err)
	}
}

func TestSetCoverRejectsFormatMismatch(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := setCover(t, path, tinyPNG(t)); err == nil {
		t.Fatal("expected rejection of PNG bytes into a .jpg cover entry, got nil")
	}
}

func TestSeriesEditPreservesExistingIndex(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Trilogy", Index: "3"} }); err != nil {
		t.Fatal(err)
	}

	book, err := save(t, path, func(b *epub.Book) { rename(b, "The Quartet") })
	if err != nil {
		t.Fatal(err)
	}

	if book.Series == nil || book.Series.Index != "3" {
		t.Errorf("series = %+v, want index 3 preserved from before the rename", book.Series)
	}
}

var opfWithAlternateScript = epubtest.OPF3.With(`    <meta refines="#creator1" property="alternate-script" xml:lang="ja">ドゥ・ジェーン</meta>`)

var opfSeriesWithIdentifier = epubtest.OPF3.With(epubtest.Metas(
	epubtest.Collection("series1", "The Trilogy", "series", "2"),
	`<meta refines="#series1" property="dcterms:identifier">urn:issn:1234-5678</meta>`,
))

func TestSeriesEditReusesCollection(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfSeriesWithIdentifier))

	book, err := save(t, path, func(b *epub.Book) { rename(b, "The Quartet") })
	if err != nil {
		t.Fatal(err)
	}
	if book.Series == nil || book.Series.Name != "The Quartet" || book.Series.Index != "2" {
		t.Fatalf("series = %+v, want The Quartet at 2", book.Series)
	}

	opfBytes, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if !bytes.Contains(opfBytes, []byte("urn:issn:1234-5678")) {
		t.Error("the collection identifier was dropped by a rename")
	}
	// Reused, so its id and the refines pointing at it survive.
	if !bytes.Contains(opfBytes, []byte(`id="series1"`)) {
		t.Error("collection id changed; refinements no longer target it")
	}
	for _, p := range []string{"collection-type", "group-position"} {
		want := []byte(`refines="#series1" property="` + p + `"`)
		if n := bytes.Count(opfBytes, want); n != 1 {
			t.Errorf("%s refines = %d, want 1 (rewritten, not duplicated)", p, n)
		}
	}

	if _, err := save(t, path, func(b *epub.Book) { rename(b, "") }); err != nil {
		t.Fatal(err)
	}
	opfBytes, ok = epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if bytes.Contains(opfBytes, []byte("series1")) {
		t.Error("clearing the series left the collection behind")
	}
}

func TestSeriesEditPreservesSets(t *testing.T) {
	opfWithSet := epubtest.OPF3.With(epubtest.Metas(
		`<!-- Series -->`,
		epubtest.Collection("series1", "The Trilogy", "series", "2"),
		"",
		`<!-- Set (bundle) -->`,
		epubtest.Collection("set1", "Complete Works", "set", ""),
	))

	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfWithSet))

	book, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Quartet", Index: "1"} })
	if err != nil {
		t.Fatal(err)
	}

	if book.Series == nil || book.Series.Name != "The Quartet" {
		t.Errorf("series = %v, want The Quartet", book.Series)
	}

	opfBytes, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}

	if !bytes.Contains(opfBytes, []byte("Complete Works")) {
		t.Error("set (Complete Works) was removed, should be preserved")
	}

	if !bytes.Contains(opfBytes, []byte(`collection-type">set`)) {
		t.Error("set collection-type was removed, should be preserved")
	}
}

// Reusing a creator element must keep an unmanaged refinement, drop a removed
// author's refinements with it, write authors in the order given, and rewrite
// managed refinements rather than duplicate them. Each step edits what the previous one left.
func TestAuthorsReuseBookkeeping(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfWithAlternateScript))

	opf := func(t *testing.T) []byte {
		t.Helper()
		b, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
		if !ok {
			t.Fatal("OPF entry not found")
		}
		return b
	}

	t.Run("rewrite", func(t *testing.T) {
		authors := []epub.Author{{Name: "Jane Doe", SortName: "Doe, Jane"}}
		book, err := save(t, path, func(b *epub.Book) { b.Authors = authors })
		if err != nil {
			t.Fatal(err)
		}
		if len(book.Authors) != 1 || book.Authors[0].Name != "Jane Doe" || book.Authors[0].SortName != "Doe, Jane" {
			t.Errorf("authors = %+v, want Jane Doe / Doe, Jane", book.Authors)
		}
		if n := bytes.Count(opf(t), []byte(`property="alternate-script"`)); n != 1 {
			t.Errorf("alternate-script count = %d, want 1", n)
		}
	})

	t.Run("reorder and clear sort name", func(t *testing.T) {
		authors := []epub.Author{{Name: "Bob Jones", SortName: "Jones, Bob"}, {Name: "Jane Doe"}}
		book, err := save(t, path, func(b *epub.Book) { b.Authors = authors })
		if err != nil {
			t.Fatal(err)
		}
		if len(book.Authors) != 2 || book.Authors[0].Name != "Bob Jones" || book.Authors[1].Name != "Jane Doe" {
			t.Fatalf("authors = %+v, want Bob Jones then Jane Doe", book.Authors)
		}
		// Jane keeps her element, so her old sort name must be cleared.
		if book.Authors[1].SortName != "" {
			t.Errorf("Jane sort name = %q, want empty", book.Authors[1].SortName)
		}

		b := opf(t)
		if n := bytes.Count(b, []byte(`property="alternate-script"`)); n != 1 {
			t.Errorf("alternate-script count = %d, want 1", n)
		}
		if n := bytes.Count(b, []byte(`refines="#creator1" property="role"`)); n != 1 {
			t.Errorf("role refines on creator1 = %d, want 1 (rewritten, not duplicated)", n)
		}
		if bytes.Contains(b, []byte(`refines="#creator1" property="file-as"`)) {
			t.Error("creator1 kept a file-as refine after its sort name was cleared")
		}
	})

	t.Run("drop", func(t *testing.T) {
		authors := []epub.Author{{Name: "Bob Jones", SortName: "Jones, Bob"}}
		if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(opf(t), []byte("creator1")) {
			t.Error("refinements still point at creator1 after the author was dropped")
		}
	})
}

// Save hands the Book the rewritten file, so it reads and saves again without
// being reopened.
func TestBookStaysUsableAfterSave(t *testing.T) {
	path := epubtest.Build(t, epubtest.OPF3)
	b := open(t, path)

	b.Title = "First Edit"
	if err := b.Save(); err != nil {
		t.Fatal(err)
	}
	opf, err := b.ReadEntry(b.PackagePath())
	if err != nil {
		t.Fatalf("ReadEntry after Save: %v", err)
	}
	if !bytes.Contains(opf, []byte("First Edit")) {
		t.Errorf("package document read after Save lacks the edit:\n%s", opf)
	}

	b.Title = "Second Edit"
	if err := b.Save(); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	if got := open(t, path).Title; got != "Second Edit" {
		t.Errorf("title on disk = %q, want Second Edit", got)
	}
}

// The read takes a legacy opf:role first and then the first role refinement, so
// a role change has to replace that one. The surviving file-as shows the element
// was reused.
func TestContributorRoleChangeIsWritten(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{
		{"epub3", epubtest.EPUB3(`    <dc:contributor id="c1">John Editor</dc:contributor>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="file-as">Editor, John</meta>`)},
		{"epub2", epubtest.EPUB2(`    <dc:contributor opf:role="edt" opf:file-as="Editor, John">John Editor</dc:contributor>`)},
		{"epub3 with a legacy opf:role", epubtest.Pkg{
			Attrs: `xmlns:opf="http://www.idpf.org/2007/opf"`,
			Meta: `    <dc:contributor id="c1" opf:role="edt">John Editor</dc:contributor>
    <meta refines="#c1" property="file-as">Editor, John</meta>`,
		}.EPUB3()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.Build(t, tc.opf)
			want := []epub.Contributor{{Name: "John Editor", Role: "trl"}}
			if _, err := save(t, path, func(b *epub.Book) { b.Contributors = want }); err != nil {
				t.Fatal(err)
			}
			if got := open(t, path).Contributors; !slices.Equal(got, want) {
				t.Errorf("contributors = %v, want %v", got, want)
			}
			if !bytes.Contains(epubtest.ReadEntry(t, path, epubtest.OPFPath), []byte("Editor, John")) {
				t.Error("file-as was dropped, so the role change replaced the contributor")
			}
		})
	}
}

// EPUB 3 gives a person one element with a role refinement per credit (D.3.10).
// EPUB 2's opf:role holds one value, so there each credit is its own element.
// Either way the person's existing element is reused.
func TestContributorCreditedTwice(t *testing.T) {
	for _, tc := range []struct {
		name     string
		opf      epubtest.PackageDoc
		elements int
	}{
		{"epub3", epubtest.EPUB3(`    <dc:contributor id="c1">Jane Doe</dc:contributor>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="file-as">Doe, Jane</meta>`), 1},
		{"epub3 with a legacy opf:role", epubtest.Pkg{
			Attrs: `xmlns:opf="http://www.idpf.org/2007/opf"`,
			Meta: `    <dc:contributor id="c1" opf:role="edt">Jane Doe</dc:contributor>
    <meta refines="#c1" property="file-as">Doe, Jane</meta>`,
		}.EPUB3(), 1},
		{"epub2", epubtest.EPUB2(`    <dc:contributor opf:role="edt" opf:file-as="Doe, Jane">Jane Doe</dc:contributor>`), 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.Build(t, tc.opf)
			want := []epub.Contributor{{Name: "Jane Doe", Role: "edt"}, {Name: "Jane Doe", Role: "trl"}}
			if _, err := save(t, path, func(b *epub.Book) { b.Contributors = want }); err != nil {
				t.Fatal(err)
			}
			if got := open(t, path).Contributors; !slices.Equal(got, want) {
				t.Errorf("contributors = %v, want %v", got, want)
			}
			if n := len(epubtest.Metadata(t, path).FindElements("//contributor")); n != tc.elements {
				t.Errorf("dc:contributor count = %d, want %d", n, tc.elements)
			}
			if !bytes.Contains(epubtest.ReadEntry(t, path, epubtest.OPFPath), []byte("Doe, Jane")) {
				t.Error("file-as was dropped, so the person's element was not reused")
			}
		})
	}
}

// Ours: one element cannot sit in two places, so a person's later entries join
// the element written for their first.
func TestContributorEntriesGatherByPersonInEPUB3(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(``))
	written := []epub.Contributor{
		{Name: "Jane Doe", Role: "edt"},
		{Name: "Bo Li", Role: "ill"},
		{Name: "Jane Doe", Role: "trl"},
	}
	if _, err := save(t, path, func(b *epub.Book) { b.Contributors = written }); err != nil {
		t.Fatal(err)
	}
	want := []epub.Contributor{written[0], written[2], written[1]}
	if got := open(t, path).Contributors; !slices.Equal(got, want) {
		t.Errorf("contributors = %v, want %v", got, want)
	}
}

// Rewriting a person's roles reuses their refinements in order and removes the
// rest, which also repairs a role repeated by an earlier writer.
func TestContributorWriteLeavesOneRefinementPerRole(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(`    <dc:contributor id="c1">Jane Doe</dc:contributor>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">trl</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>`))
	if _, err := save(t, path, func(b *epub.Book) {
		b.Contributors = []epub.Contributor{{Name: "Jane Doe", Role: "trl"}}
	}); err != nil {
		t.Fatal(err)
	}

	var roles []string
	for _, m := range epubtest.Metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("refines", "") == "#c1" && m.SelectAttrValue("property", "") == "role" {
			roles = append(roles, m.Text())
		}
	}
	if !slices.Equal(roles, []string{"trl"}) {
		t.Errorf("role refinements = %v, want [trl] once", roles)
	}
}

// synctest's clock moves only when the test sleeps, so the stamp can be checked exactly.
func TestModifiedStampIsWrittenOnlyForARealChange(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
		opfOf := func() []byte {
			t.Helper()
			b, _ := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
			return b
		}

		title := "A New Title"
		if _, err := save(t, path, func(b *epub.Book) { b.Title = title }); err != nil {
			t.Fatal(err)
		}
		want := []byte(`<meta property="dcterms:modified">2000-01-01T00:00:00Z</meta>`)
		if !bytes.Contains(opfOf(), want) {
			t.Errorf("stamp is not the time of the edit:\n%s", opfOf())
		}

		time.Sleep(time.Hour)
		if _, err := save(t, path, func(b *epub.Book) { b.Title = title }); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(opfOf(), want) {
			t.Errorf("a no-op edit restamped dcterms:modified:\n%s", opfOf())
		}
	})
}

func TestSortTitleKeepsTheCalibreMetaInStepForEPUB3(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3.With(`    <meta name="calibre:title_sort" content="Stale, The"/>`)))
	book, err := save(t, path, func(b *epub.Book) { b.SortTitle = "Fresh, The" })
	if err != nil {
		t.Fatal(err)
	}
	if book.SortTitle != "Fresh, The" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "Fresh, The")
	}

	opf, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if bytes.Contains(opf, []byte("Stale, The")) {
		t.Errorf("calibre:title_sort left contradicting the refinement:\n%s", opf)
	}
	if !bytes.Contains(opf, []byte(`<meta name="calibre:title_sort" content="Fresh, The"/>`)) {
		t.Errorf("calibre:title_sort not kept in step:\n%s", opf)
	}
	if !bytes.Contains(opf, []byte(`property="file-as">Fresh, The<`)) {
		t.Errorf("file-as refinement not updated:\n%s", opf)
	}
}

func jpegSized(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func coverPageEpub(t *testing.T, opf epubtest.PackageDoc, page string, w, h int) string {
	t.Helper()
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opf, epubtest.Entry{Name: "OEBPS/cover.xhtml", Data: []byte(page)}))
	if _, err := setCover(t, path, jpegSized(t, w, h)); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCoverPageRefitByTheGuideReference(t *testing.T) {
	// The spine opens on a chapter, so only the guide reaches the cover page.
	path := coverPageEpub(t, coverPageInGuide, epubtest.SVGCoverPage, 1200, 1600)

	page := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml"))
	for _, want := range []string{
		`viewBox="0 0 1200 1600"`,
		`width="1200"`,
		`height="1600"`,
		// The svg fills the viewport at any image size, so these stay.
		`width="100%"`,
		`preserveAspectRatio="xMidYMid meet"`,
		`<!DOCTYPE html>`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("cover page is missing %s:\n%s", want, page)
		}
	}
	if strings.Contains(page, "600 800") {
		t.Errorf("cover page still carries the old dimensions:\n%s", page)
	}
}

func TestCoverPageRefitByTheFirstSpineItem(t *testing.T) {
	path := coverPageEpub(t, coverPageInSpine, epubtest.SVGCoverPage, 1200, 1600)

	if page := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml")); !strings.Contains(page, `viewBox="0 0 1200 1600"`) {
		t.Errorf("cover page was not refitted:\n%s", page)
	}
}

func TestCoverPageUntouched(t *testing.T) {
	const noStatedDimensions = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Cover</title><style>img { width: 100%; }</style></head>
<body><img src="cover.jpg" alt="Cover"/></body>
</html>`
	const percentDimensions = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Cover</title></head>
<body><img src="cover.jpg" width="100%" height="100%" alt="Cover"/></body>
</html>`

	for _, tc := range []struct {
		name string
		page string
		w, h int
	}{
		{"draws something else", strings.Replace(epubtest.SVGCoverPage, "cover.jpg", "frontispiece.jpg", 1), 1200, 1600},

		{"states no dimensions", noStatedDimensions, 1200, 1600},

		{"states percentages", percentDimensions, 1200, 1600},

		{"replacement is the same size", epubtest.SVGCoverPage, 600, 800},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := coverPageEpub(t, coverPageInGuide, tc.page, tc.w, tc.h)

			if got := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml")); got != tc.page {
				t.Errorf("cover page was rewritten:\n%s", got)
			}
		})
	}
}

// §6.1.2 allows any HTML named entity, so &nbsp; must not read as broken.
func TestCoverPageWithAnHTMLEntityIsRefitted(t *testing.T) {
	page := strings.Replace(epubtest.SVGCoverPage, "<title>Cover</title>", "<title>Cover&nbsp;Page</title>", 1)
	path := coverPageEpub(t, coverPageInGuide, page, 1200, 1600)

	if got := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml")); !strings.Contains(got, `viewBox="0 0 1200 1600"`) {
		t.Errorf("cover page was not refitted:\n%s", got)
	}
}

func TestCoverPageImgAttributesAreRefitted(t *testing.T) {
	const page = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Cover</title></head>
<body><img src="cover.jpg" width="600" height="800" alt="Cover"/></body>
</html>`
	path := coverPageEpub(t, coverPageInGuide, page, 1200, 1600)

	got := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml"))
	if !strings.Contains(got, `width="1200"`) || !strings.Contains(got, `height="1600"`) {
		t.Errorf("img was not refitted:\n%s", got)
	}
	if !strings.Contains(got, `alt="Cover"`) {
		t.Errorf("img lost an attribute that was not ours:\n%s", got)
	}
}

// docs/DECISIONS.md #23 says why every signed epub is refused.
func TestRefusesToEditASignedEpub(t *testing.T) {
	const signatures = `<?xml version="1.0"?>
<signatures xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><Signature/></signatures>`
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3, epubtest.Entry{Name: "META-INF/signatures.xml", Data: []byte(signatures)}))

	title := "New Title"
	cover := tinyJPEG(t)
	// The closure runs inside t.Run, where the outer t's Fatal would panic, so
	// the subtest checks the error instead.
	var coverErr error
	for _, tc := range []struct {
		name string
		e    func(*epub.Book)
	}{
		{"bib edit", func(b *epub.Book) { b.Title = title }},
		{"cover edit", func(b *epub.Book) { coverErr = b.SetCover(cover) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := save(t, path, tc.e)
			if coverErr != nil {
				t.Fatalf("SetCover: %v", coverErr)
			}
			if err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Original Title" {
		t.Errorf("title = %q, want the original", bib.Title)
	}
	if got := epubtest.ReadEntry(t, path, "OEBPS/cover.jpg"); !bytes.Equal(got, epubtest.CoverBytes) {
		t.Errorf("cover = %q, want the original", got)
	}
}

func TestCoverPageCDataStyleSurvivesARefit(t *testing.T) {
	const page = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Cover</title><style type="text/css"><![CDATA[
body { margin: 0; }
div > img { width: 100%; }
]]></style></head>
<body><div><img src="cover.jpg" width="600" height="800" alt="Cover"/></div></body>
</html>`
	path := coverPageEpub(t, coverPageInGuide, page, 1200, 1600)

	got := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml"))
	if !strings.Contains(got, `width="1200"`) {
		t.Errorf("img was not refitted:\n%s", got)
	}
	if !strings.Contains(got, "<![CDATA[") || !strings.Contains(got, "div > img") {
		t.Errorf("the stylesheet was re-encoded:\n%s", got)
	}
	if strings.Contains(got, "&gt;") {
		t.Errorf("the stylesheet's > was escaped:\n%s", got)
	}
}

// Our rule: §5.3.6 binds a refinement by id and says nothing about a renamed value.
func TestAuthorRenameDropsRefinements(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfWithAlternateScript))

	authors := []epub.Author{{Name: "Jane Smith", SortName: "Smith, Jane"}}
	book, err := save(t, path, func(b *epub.Book) { b.Authors = authors })
	if err != nil {
		t.Fatal(err)
	}
	if len(book.Authors) != 1 || book.Authors[0].Name != "Jane Smith" {
		t.Fatalf("authors = %+v, want Jane Smith", book.Authors)
	}

	opfBytes, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if bytes.Contains(opfBytes, []byte("ドゥ・ジェーン")) {
		t.Error("an alternate-script written about the old name survived the rename")
	}
	if !bytes.Contains(opfBytes, []byte("Smith, Jane")) {
		t.Error("the supplied sort name was not written")
	}
}
