// What Save writes, and what it leaves alone.
//
// The rules under test:
//
//   - an edit preserves every piece of metadata it was not asked to change,
//     including metadata this package did not write and does not understand;
//   - an edit round-trips, so Open reads back what the edit asked for;
//   - an edit is idempotent, so applying it twice accumulates no duplicates and
//     churns the file no further.
//
// Past those table-driven tests, the rest of the file pins choices rather than
// rules: a calibre convention, a deliberate divergence, a known-wrong behaviour
// held still. Each says in its own comment what would justify changing it.
// Conformance assertions live in spec_ext_test.go.

package epub_test

import (
	"bytes"
	"image"
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
	// sortTitle records whether the package can carry one at all. EPUB 2 has no
	// standard mechanism and ebookfs writes no proprietary fallback, so the edit
	// is silently discarded there.
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
				// Presence only: ebookfs writes this one, so it is not foreign
				// metadata by this list's own definition. Every rewrite updates
				// it, and TestSpecModifiedIsUpdated owns the value and
				// the format. What matters here is that an edit does not drop
				// or duplicate it.
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

// edits that change one field and must disturb nothing else. Deliberately
// excluded: clearing the series and renaming an author, which are *supposed* to
// take the series identifier and the alternate-script with them (the entity
// they describe is gone). Those live in the internal tests.
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

// assertOutsideMetadataUnchanged pins the widest form of the preservation rule:
// a metadata edit changes nothing outside <metadata>. Serializing the manifest
// and the spine and requiring them byte-identical catches more than naming the
// attributes would: a dropped properties="cover-image", a lost spine toc, or a
// reordered item, and needs no list to keep in step with the fixtures.
//
// Only metadata edits. A cover edit is supposed to touch the manifest, and the
// tests that own that behaviour assert it themselves.
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

// A sort title describes the title the file has, not the book, so writing a
// title does not touch it. Dropping a stale one on a rename is a library policy
// and lives in the adapter's suite, which is why this states the opposite.
func TestTitleEditLeavesTheSortTitle(t *testing.T) {
	for _, c := range corpora() {
		if !c.sortTitle {
			continue // no mechanism in this package version
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
		name string
		e    func(*epub.Book)
		// sortTitle marks the case that writes one, skipped on a package
		// version with no mechanism for it.
		sortTitle bool
		check     func(*testing.T, *epub.Book)
	}{
		{"title", func(b *epub.Book) { b.Title = "New Title" }, false, func(t *testing.T, b *epub.Book) {
			if b.Title != "New Title" {
				t.Errorf("title = %q", b.Title)
			}
		}},
		{"sort title", func(b *epub.Book) { b.SortTitle = "Title, New" }, true, func(t *testing.T, b *epub.Book) {
			if b.SortTitle != "Title, New" {
				t.Errorf("sort title = %q", b.SortTitle)
			}
		}},
		{"description", func(b *epub.Book) { b.Description = "A new description." }, false, func(t *testing.T, b *epub.Book) {
			if b.Description != "A new description." {
				t.Errorf("description = %q", b.Description)
			}
		}},
		{"language", func(b *epub.Book) { b.Language = "fr" }, false, func(t *testing.T, b *epub.Book) {
			if b.Language != "fr" {
				t.Errorf("language = %q", b.Language)
			}
		}},
		{"authors", func(b *epub.Book) { b.Authors = authors }, false, func(t *testing.T, b *epub.Book) {
			if len(b.Authors) != 2 || b.Authors[0].Name != "Ann Rand" || b.Authors[1].Name != "Bo Li" {
				t.Fatalf("authors = %+v", b.Authors)
			}
			if b.Authors[0].SortName != "Rand, Ann" || b.Authors[1].SortName != "" {
				t.Errorf("sort names = %q, %q", b.Authors[0].SortName, b.Authors[1].SortName)
			}
		}},
		{"series rename keeps position", func(b *epub.Book) { rename(b, "The Quartet") }, false, func(t *testing.T, b *epub.Book) {
			if b.Series == nil || b.Series.Name != "The Quartet" || b.Series.Index != "2" {
				t.Errorf("series = %+v, want The Quartet at 2", b.Series)
			}
		}},
		{"series index keeps name", func(b *epub.Book) { reposition(b, "4") }, false, func(t *testing.T, b *epub.Book) {
			if b.Series == nil || b.Series.Name != "The Trilogy" || b.Series.Index != "4" {
				t.Errorf("series = %+v, want The Trilogy at 4", b.Series)
			}
		}},
		{"series cleared", func(b *epub.Book) { rename(b, "") }, false, func(t *testing.T, b *epub.Book) {
			if b.Series != nil {
				t.Errorf("series = %+v, want nil", b.Series)
			}
		}},
	}

	for _, c := range corpora() {
		for _, tc := range cases {
			if tc.sortTitle && !c.sortTitle {
				continue // no mechanism in this package version
			}
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				path := epubtest.Build(t, c.opf)
				bib, err := save(t, path, tc.e)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, bib)

				// Save leaves the Book reading the rewritten file, so
				// agreeing with a fresh Open is the claim that the write
				// landed on disk rather than only in memory.
				fresh, err := parse(t, path)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, fresh)
			})
		}
	}
}

// The second write must produce byte-identical OPF, or the writer appends where
// it should replace and repeated edits grow the file forever. synctest freezes
// the clock so dcterms:modified cannot differ for the trivial reason that a
// second elapsed.
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

// No fixture above rebinds a prefix, so none takes a second edit on a document
// spelling our property differently from how we would write it fresh. The first
// edit declares dcterms2; every later one must recognise that element as ours
// or mint dcterms3, dcterms4, growing the package element once per save.
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
		// Theirs, untouched, plus exactly one of ours.
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

// --- packages carrying both series encodings ---
//
// A version="2.0" package may carry a belongs-to-collection meta (OPF 2.0
// §2.2.10 lets <meta> carry anything), and the reader prefers the EPUB 3
// collection whatever the version says. So a file can hold the series twice, in
// two encodings that disagree.

func TestEPUB2SeriesEditUpdatesBothEncodings(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB2(epubtest.Metas(
		`<dc:creator opf:role="aut">Ann Rand</dc:creator>`,
		epubtest.Collection("c01", "The Old Series", "series", "2"),
	)))

	// The collection outranks the calibre metas on read, v2 package or not.
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

// A v3 package carrying calibre metas has them brought into step rather than
// left asserting a series the collection no longer names. A calibre reader
// consults them first.
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

// A file that never carried the proprietary encoding does not acquire it.
// "Keep every encoding in step" is not licence to add one.
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

// assertSeriesEncodings checks that every encoding present in the document
// names the same series, and that exactly one collection survives.
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

// EPUB 2 has no group-position, so the series goes into calibre:series_index,
// a float by calibre's convention. Writing the first two levels is a deliberate
// narrowing, not a silent collapse to 1.
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

// --- repeatable Dublin Core elements ---
//
// EPUB 3.3 §5.5.3.2.1 makes the optional DCMES elements "OPTIONAL child of
// metadata. Repeatable." The spec is silent on which to present.
//
// Every other repeatable field read here is first-wins (title §5.5.3.1.2,
// creator order §5.5.3.2.3, language §5.5.3.1.3) and calibre takes the first,
// but opfMetadata.Description is a plain string so encoding/xml keeps the last.
// That is emergent, not chosen.

// A collection with no group-position, in a file still holding a stale
// calibre:series_index. The carried-over position is empty, so the stale index
// goes rather than staying to contradict the collection.
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

// --- an EPUB 2 creator can lose its sort name ---

// Same author name, sort name cleared. The creator element is reused, so its
// opf:file-as has to go rather than stay behind describing a sort order the
// caller just cleared.
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

// An EPUB 3 package whose creators still carry the EPUB 2 opf:file-as, common in
// v2 files upgraded in place. Carrying both is allowed; disagreeing with itself
// is not. The read prefers the attribute and a v3 write touches only the
// refinement, so the edit would land where the read never looks and the stale
// value would be reported forever.
func TestEPUB3CreatorWithALegacySortNameTakesTheEdit(t *testing.T) {
	opf := epubtest.PackageDoc(strings.Replace(string(epubtest.EPUB3(`    <dc:creator id="c1" opf:file-as="Stale, Name">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>`)),
		`xmlns:dc="http://purl.org/dc/elements/1.1/"`,
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf"`, 1))

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want the sort name the edit asked for", bib.Authors)
	}
	// Whichever mechanism the writer picks, the file must not end up claiming
	// both. A stale attribute left beside a fresh refinement is that failure.
	c := epubtest.Metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "" && got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want it updated or removed, not left stale", got)
	}
}

// A creator carrying a bare file-as rather than opf:file-as. Reading matches the
// local name and finds it; writing always qualifies with opf, so the update
// lands on a second attribute and leaves the one the read prefers untouched.
//
// The element then asserts two sort names and the next read takes the stale one.
func TestUnprefixedFileAsIsUpdatedNotDuplicated(t *testing.T) {
	opf := epubtest.EPUB2(`    <dc:creator opf:role="aut" file-as="Stale, Name">Ann Rand</dc:creator>`)

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want the sort name the edit asked for", bib.Authors)
	}

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

// --- recovering from an empty value ---
//
// §5.5.2 makes an empty dc:title, creator or date invalid, and there the spec
// stops. What a reader does with one is unstated, so recovering rather than
// rejecting is our choice; these tests sit outside the conformance assertions.

// §5.5.2 requires non-empty values, so a file whose only dc:description is empty
// is malformed and no rule says where a write goes. Reusing that element rather
// than adding a second leaves one description however many rewrites it sees.
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
	// The reader skips the empty element the write landed in front of.
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != desc {
		t.Errorf("description = %q, want the edit read back", bib.Description)
	}
}

// --- a repeated author name ---

// --- refinements need a target ---

// A known gap. D.3.6 gives file-as "Cardinality: zero or one", so two on one
// creator is malformed. The writer updates the first and leaves the second
// contradicting it; the reader takes the first. Stripping every refinement to
// append a replacement is the churn this package avoids everywhere else. Delete
// this test if a file turns up where it matters.
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
	// The reader takes the same one the writer updated, so the duplicate is
	// inert rather than contradicting what the edit asked for.
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("sort name = %q, want the refinement the write updated", bib.Authors[0].SortName)
	}
}

// --- identifiers ---

// The language and direction a publisher put on a metadata element survive an
// edit to a different field. They are attributes, so this holds only while
// elements are reused rather than rebuilt. Nothing in §5.3.1 or §5.3.7 requires
// preserving them.
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

// --- metadata we do not model ---

// Dublin Core elements ebookfs has no field for are still carried through an
// edit untouched. Not modelling something is not a licence to drop it.
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

// --- a collection-type we do not own ---

// Both sides resolve to the first unschemed refinement, since series and set
// are defined only "when no scheme is specified" (D.3.4), so a value from
// someone else's code list is left alone.
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

// --- renaming a series the reader cannot see ---

// A collection the reader fails to resolve has a write-side consequence: the
// writer cannot see it either, so a rename adds a second one instead of
// rewriting the first, and the new one carries no group-position, resetting the
// book's position to 1.
//
// The fixture is the spec whitespace one, whose whole reason for escaping the
// reader is spelled out there. Pinned here is only our rule: one collection,
// position preserved across a rename.
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

// Neither spec makes <docTitle> and <docAuthor> track dc:title and dc:creator,
// so these pin our rule: an edit keeps them in step, and never creates one that
// was not there.

func ncxWith(body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <head><meta name="dtb:uid" content="urn:uuid:1234"/></head>
` + body + `
  <navMap><navPoint id="p1" playOrder="1"><navLabel><text>Chapter One</text></navLabel><content src="chapter1.xhtml"/></navPoint></navMap>
</ncx>`
}

// buildNCXEpub writes the standard archive with an NCX beside the package
// document, and returns the epub's path.
func buildNCXEpub(t *testing.T, ncx string) string {
	t.Helper()
	return epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.NCXOPF, epubtest.Entry{Name: "OEBPS/toc.ncx", Data: []byte(ncx)}))
}

// ncxTexts returns the <text> values under every element with the given tag, so
// a test can assert on what the NCX now says without walking etree itself.
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

// ncxWithTitle is the NCX the tests below share: a document title and nothing
// else, so an edit either reaches <docTitle> or leaves the file alone.
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

	// The added ones must land before <navMap>, which the NCX content model
	// puts after docAuthor: appending to <ncx> would have made the file invalid.
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

// Where a first <docAuthor> would go is the content model's business, not ours.
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

// A book with no NCX at all is the EPUB 3 norm, and an edit must not fail over
// the manifest not declaring one.
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

// The NCX is read only for a field it carries, so a series edit leaves the
// entry byte for byte as it was.
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

// An unreadable NCX does not fail the edit, and is left exactly as it was. The
// second case is malformed only in its nesting, the kind of document etree used
// to correct silently.
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

// A CDATA section is a spelling of a value, not a different value, so a title
// edit has no business re-encoding it. Nothing else in the corpus uses CDATA.
func TestCDataDescriptionKeepsItsSpelling(t *testing.T) {
	// Inside a CDATA section &amp; is five literal characters, not an escape,
	// so this is also the value the reader must report.
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

// --- multipart titles ---
//
// The fixture is §5.5.3.1.2's own example. The second element is another segment
// of the same title, so replacing the title has to take it too.

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

// Only a title write takes them: a sort title is a property of the title the
// file already has, not a claim the book was renamed.
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

// The second edit finds nothing to drop, so it leaves the file alone.
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

// --- WriteBib ---

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
			// Re-parse from disk independently to confirm it persisted.
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
			// A fractional index (e.g. a 1.5 novella) must round-trip, not truncate.
			book, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Saga", Index: "1.5"} })
			if err != nil {
				t.Fatal(err)
			}
			if book.Series == nil || book.Series.Name != "The Saga" {
				t.Errorf("series = %v, want The Saga", book.Series)
			}
			if book.Series == nil || book.Series.Index != "1.5" {
				t.Errorf("series index = %v, want 1.5", book.Series.Index)
			}

			// Clearing it removes the series.
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

	// Stored as the standard EPUB 3 file-as refine, never calibre:title_sort.
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
	// Setting only the sort title must not disturb the title.
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
	// EPUB 2 has no standard sort-title mechanism, so the proprietary meta calibre
	// writes is used instead, the same fallback this package already uses for the
	// series. A refinement is an EPUB 3 construct and must not appear.
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

	// Clearing it takes the meta with it rather than leaving a stale value.
	if _, err := save(t, path, func(b *epub.Book) { b.SortTitle = "" }); err != nil {
		t.Fatal(err)
	}
	opf, _ = epubtest.ReadEntryFromFile(t, path, "OEBPS/content.opf")
	if bytes.Contains(opf, []byte("calibre:title_sort")) {
		t.Errorf("a title change left a stale calibre:title_sort:\n%s", opf)
	}
}

func TestSaveAcceptsValidLanguageVerbatim(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	// A recognised tag is accepted and written through verbatim: validated, not
	// canonicalised (calibre would rewrite "pt-BR" to a 3-letter code).
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

// CipherReference/@URI is a URL attribute, so "OEBPS/my book.opf" is declared
// "OEBPS/my%20book.opf". Undecoded, the map is keyed by a name no entry has and
// the edit rewrites a genuinely encrypted package document.
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

// The fail-open case. A producer writing an unencoded name into both
// encryption.xml and the zip has an entry whose name really contains "%20", so
// the decoded form matches nothing and an encrypted entry reads as readable.
func TestSaveRefusesEncryptedEntryNamedLiterally(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/a%20b.opf", epubtest.PackageMediaType)
	enc := epubtest.EncryptionXML(epubtest.AES256, "OEBPS/a%20b.opf")

	// The entry name contains the percent-encoding literally, so the raw value is
	// what matches and the decoded one does not.
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
	// Font obfuscation looks like encryption but must not block metadata edits.
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
	newCover := tinyJPEG(t)
	if _, err := setCover(t, path, newCover); err != nil {
		t.Fatal(err)
	}
	got, err := open(t, path).Cover()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newCover) {
		t.Errorf("cover = %q, want the supplied JPEG bytes", got)
	}
	// A cover swap leaves the chapter untouched.
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

func TestSetCoverRefusesNonRaster(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := setCover(t, path, []byte("<svg/>")); err == nil {
		t.Fatal("expected refusal on non-raster cover format, got nil")
	}
}

func TestSetCoverRejectsNonImage(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := setCover(t, path, []byte("definitely not an image")); err == nil {
		t.Fatal("expected rejection of non-image cover data, got nil")
	}
}

func TestSetCoverRejectsFormatMismatch(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	// PNG bytes into a .jpg cover entry must be rejected. We do not transcode.
	if _, err := setCover(t, path, tinyPNG(t)); err == nil {
		t.Fatal("expected rejection of PNG bytes into a .jpg cover entry, got nil")
	}
}

// --- Metadata we must not clobber ---

func TestSeriesEditPreservesExistingIndex(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	// Set up a series with index 3, then rename the series without setting an
	// index. The index must survive as 3, not reset to 1.
	if _, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Trilogy", Index: "3"} }); err != nil {
		t.Fatal(err)
	}

	book, err := save(t, path, func(b *epub.Book) { rename(b, "The Quartet") })
	if err != nil {
		t.Fatal(err)
	}

	if book.Series == nil || book.Series.Index != "3" {
		t.Errorf("series index = %v, want 3.0 (preserved from before rename)", book.Series.Index)
	}
}

// opfWithAlternateScript carries a refinement ebookfs does not manage
// (alternate-script, a publisher/Calibre convention) alongside the role and
// file-as it does.
var opfWithAlternateScript = epubtest.OPF3.With(`    <meta refines="#creator1" property="alternate-script" xml:lang="ja">ドゥ・ジェーン</meta>`)

// opfSeriesWithIdentifier refines the collection with a dcterms:identifier.
// EPUB 3 lets a series carry an ISSN, which is not ours to rewrite.
var opfSeriesWithIdentifier = epubtest.OPF3.With(epubtest.Metas(
	epubtest.Collection("series1", "The Trilogy", "series", "2"),
	`<meta refines="#series1" property="dcterms:identifier">urn:issn:1234-5678</meta>`,
))

// A series edit rewrites the collection it found, so an unmanaged refinement
// survives. Clearing the series still takes the whole thing.
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
	// The element is reused, so it keeps its own id rather than being minted a
	// fresh one, and the refines above still point at it.
	if !bytes.Contains(opfBytes, []byte(`id="series1"`)) {
		t.Error("collection id changed; refinements no longer target it")
	}
	for _, p := range []string{"collection-type", "group-position"} {
		want := []byte(`refines="#series1" property="` + p + `"`)
		if n := bytes.Count(opfBytes, want); n != 1 {
			t.Errorf("%s refines = %d, want 1 (rewritten, not duplicated)", p, n)
		}
	}

	// Clearing the series takes the element with it, identifier and all.
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

	// A series edit updates the collection and leaves the set beside it.
	book, err := save(t, path, func(b *epub.Book) { b.Series = &epub.Series{Name: "The Quartet", Index: "1"} })
	if err != nil {
		t.Fatal(err)
	}

	if book.Series == nil || book.Series.Name != "The Quartet" {
		t.Errorf("series = %v, want The Quartet", book.Series)
	}

	// The set survives the series edit.
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

// What reusing a creator element has to get right: an unmanaged refinement
// survives, a dropped author takes its refinements with it, the written order
// is the order given, and managed refinements are rewritten rather than
// duplicated. Each step edits what the previous one left.
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
		// The author list is unchanged, so the creator is reused as-is.
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
		// Jane kept her element, so her sort name has to be cleared rather than
		// left over from the previous write.
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
		// Jane's element goes, and so must everything refining it.
		authors := []epub.Author{{Name: "Bob Jones", SortName: "Jones, Bob"}}
		if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(opf(t), []byte("creator1")) {
			t.Error("refinements still point at creator1 after the author was dropped")
		}
	})
}

// synctest's clock moves only when the test sleeps, so the stamp is an exact
// value rather than a format check: a real edit records the time, an edit
// asking for what the file already says records nothing, even an hour later.
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

		// Same title an hour later: nothing changes, so nothing is restamped.
		time.Sleep(time.Hour)
		if _, err := save(t, path, func(b *epub.Book) { b.Title = title }); err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(opfOf(), want) {
			t.Errorf("a no-op edit restamped dcterms:modified:\n%s", opfOf())
		}
	})
}

// The calibre meta is updated when the file already carries one, so it cannot
// contradict the refinement, and is never injected into a file without one.
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

// --- cover page dimensions ---
//
// Replacing the cover leaves the page displaying it claiming the old image's
// dimensions; package content says why that crops. These pin the repair and its
// limits: only where the page already said something, and only on the real
// cover page.

func jpegSized(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// coverPageEpub builds a book whose cover page is page, and replaces the cover
// with a w by h image. It returns the epub's path.
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
		// Untouched: the svg fills the viewport at any image size, and the
		// doctype and the rest of the document are not ours to rewrite.
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
	// No guide at all, the EPUB 3 norm.
	path := coverPageEpub(t, coverPageInSpine, epubtest.SVGCoverPage, 1200, 1600)

	if page := string(epubtest.ReadEntry(t, path, "OEBPS/cover.xhtml")); !strings.Contains(page, `viewBox="0 0 1200 1600"`) {
		t.Errorf("cover page was not refitted:\n%s", page)
	}
}

// The three shapes a refit leaves alone. Each asserts the entry comes back byte
// for byte, since a cover page is a content document and rewriting one that
// needed nothing is a change we cannot justify.
func TestCoverPageUntouched(t *testing.T) {
	const noStatedDimensions = `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
<head><title>Cover</title><style>img { width: 100%; }</style></head>
<body><img src="cover.jpg" alt="Cover"/></body>
</html>`

	for _, tc := range []struct {
		name string
		page string
		w, h int
	}{
		// A candidate not drawing the cover image is not the cover page, whatever
		// the guide says.
		{"draws something else", strings.Replace(epubtest.SVGCoverPage, "cover.jpg", "frontispiece.jpg", 1), 1200, 1600},

		// A page sizing its cover in CSS is already correct at any size, and the
		// repair only updates what the document already stated.
		{"states no dimensions", noStatedDimensions, 1200, 1600},

		// A same-sized replacement leaves nothing to refit, so the entry is copied.
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

// The other spelling: an HTML <img> sized by attributes, its src resolved the
// same way an SVG image's href is.
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

// --- signed containers ---

// Every entry an edit replaces may be one a signature covers, and we cannot
// re-sign. DECISIONS.md #23 says why the check is not narrower than this.
func TestRefusesToEditASignedEpub(t *testing.T) {
	const signatures = `<?xml version="1.0"?>
<signatures xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><Signature/></signatures>`
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3, epubtest.Entry{Name: "META-INF/signatures.xml", Data: []byte(signatures)}))

	title := "New Title"
	cover := tinyJPEG(t)
	for _, tc := range []struct {
		name string
		e    func(*epub.Book)
	}{
		{"bib edit", func(b *epub.Book) { b.Title = title }},
		{"cover edit", func(b *epub.Book) {
			if err := b.SetCover(cover); err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := save(t, path, tc.e); err == nil {
				t.Fatal("expected a refusal")
			}
		})
	}

	// Untouched, down to the entry that would have been rewritten first.
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

// A refit must resize the image without re-encoding the CDATA a <style> block
// uses to hold < and & unescaped; package content says why that matters.
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

// A renamed author gets a fresh creator rather than the old one relabelled, so
// a refinement written about the old name does not end up describing the new
// one.
//
// Ours, not the spec's. §5.3.6 binds a refinement to an element by id and says
// nothing about what an editor should do when the value under that id changes.
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
	// The sort name the edit supplied is written, so the new creator is not
	// simply bare.
	if !bytes.Contains(opfBytes, []byte("Smith, Jane")) {
		t.Error("the supplied sort name was not written")
	}
}
