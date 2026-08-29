// Package epub_test drives the package from the outside, through Rewrite and
// Parse only, and pins what an edit is allowed to do to a file — the part that
// must not change.
//
// The rules under test:
//
//   - an edit preserves every piece of metadata it was not asked to change,
//     including metadata ebookfs did not write and does not understand;
//   - an edit round-trips, i.e. Parse reads back what the edit asked for;
//   - an edit is idempotent, so applying it twice does not accumulate
//     duplicates or churn the file further.
//
// Past the table-driven tests carrying those rules, the rest of the file pins
// choices rather than rules: a calibre convention, a deliberate divergence, a
// known-wrong behaviour held still. Those are ours to revisit, and each says in
// its own comment what would justify changing it. Conformance assertions live in
// spec_ext_test.go.
package epub_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"testing/synctest"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// item is a piece of metadata no part of ebookfs writes, reads, or understands:
// a publisher's alternate script for an author, a series ISSN, a set the book
// belongs to, an editor, a calibre column. An edit that changes something else
// must leave every one of them exactly where it was. Both bugs this suite
// exists to catch — dropped creator refinements, a dropped series identifier —
// were failures of that rule.
type item struct {
	name string
	path string // etree path, relative to <metadata>
	text string // expected element text; "" to assert presence only
}

type corpus struct {
	name    string
	opf     string
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
			opf:       richOPF3,
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
			opf:  richOPF2,
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

const richOPF3 = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <!-- a comment nobody should eat -->
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <dc:title id="t1">Original Title</dc:title>
    <meta refines="#t1" property="file-as">Title, Original</meta>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#creator1" property="file-as">Doe, Jane</meta>
    <meta refines="#creator1" property="alternate-script" xml:lang="ja">ドゥ・ジェーン</meta>
    <dc:contributor id="ed1">An Editor</dc:contributor>
    <meta refines="#ed1" property="role" scheme="marc:relators">edt</meta>
    <dc:language>en</dc:language>
    <dc:date>2020-01-02</dc:date>
    <dc:description>Original description.</dc:description>
    <meta property="dcterms:modified">2020-01-02T00:00:00Z</meta>
    <meta property="belongs-to-collection" id="series1">The Trilogy</meta>
    <meta refines="#series1" property="collection-type">series</meta>
    <meta refines="#series1" property="group-position">2</meta>
    <meta refines="#series1" property="dcterms:identifier">urn:issn:1234-5678</meta>
    <meta property="belongs-to-collection" id="set1">Complete Works</meta>
    <meta refines="#set1" property="collection-type">set</meta>
    <meta name="calibre:user_rating" content="8"/>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

const richOPF2 = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <!-- a comment nobody should eat -->
    <dc:identifier id="bookid" opf:scheme="uuid">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="creator1" opf:role="aut" opf:file-as="Doe, Jane">Jane Doe</dc:creator>
    <dc:contributor id="ed1" opf:role="edt">An Editor</dc:contributor>
    <dc:language>en</dc:language>
    <dc:date opf:event="publication">2020-01-02</dc:date>
    <dc:description>Original description.</dc:description>
    <meta name="calibre:series" content="The Trilogy"/>
    <meta name="calibre:series_index" content="2"/>
    <meta name="calibre:user_rating" content="8"/>
    <meta name="publisher:internal-id" content="ACME-99"/>
    <meta name="cover" content="cover-img"/>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`

// edits that change one field and must disturb nothing else. Deliberately
// excluded: clearing the series and renaming an author, which are *supposed* to
// take the series identifier and the alternate-script with them (the entity
// they describe is gone). Those live in the internal tests.
func preservingEdits() []struct {
	name string
	e    model.Edits
} {
	s := func(v string) *string { return &v }
	authors := []model.Author{{Name: "Jane Doe", SortName: "Doe, Jane"}}
	return []struct {
		name string
		e    model.Edits
	}{
		{"title", model.Edits{Title: s("New Title")}},
		{"sort title", model.Edits{SortTitle: s("Title, New")}},
		{"description", model.Edits{Description: s("A new description.")}},
		{"language", model.Edits{Language: s("fr")}},
		{"authors unchanged", model.Edits{Authors: &authors}},
		{"series rename", model.Edits{Series: s("The Quartet")}},
		{"series index", model.Edits{SeriesIndex: s("4")}},
	}
}

func TestRewritePreservesForeignMetadata(t *testing.T) {
	for _, c := range corpora() {
		for _, tc := range preservingEdits() {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				path := buildEpub(t, c.opf)
				if _, err := epub.Rewrite(path, book(t, path), tc.e); err != nil {
					t.Fatal(err)
				}
				opf := readEntry(t, path, opfPath)

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
// attributes would — a dropped properties="cover-image", a lost spine toc, a
// reordered item — and needs no list to keep in step with the fixtures.
//
// Only metadata edits. A cover edit is supposed to touch the manifest, and the
// tests that own that behaviour assert it themselves.
func assertOutsideMetadataUnchanged(t *testing.T, before string, after []byte) {
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

func TestRewriteRoundTrips(t *testing.T) {
	s := func(v string) *string { return &v }
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}, {Name: "Bo Li"}}

	cases := []struct {
		name  string
		e     model.Edits
		check func(*testing.T, bookmodel.Bib)
	}{
		{"title", model.Edits{Title: s("New Title")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Title != "New Title" {
				t.Errorf("title = %q", b.Title)
			}
			// Documented rule: a title change without a new sort title clears
			// the old one, which described the old title.
			if b.SortTitle != "" {
				t.Errorf("sort title = %q, want cleared by the title change", b.SortTitle)
			}
		}},
		{"sort title", model.Edits{SortTitle: s("Title, New")}, func(t *testing.T, b bookmodel.Bib) {
			if b.SortTitle != "Title, New" {
				t.Errorf("sort title = %q", b.SortTitle)
			}
		}},
		{"description", model.Edits{Description: s("A new description.")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Description != "A new description." {
				t.Errorf("description = %q", b.Description)
			}
		}},
		{"language", model.Edits{Language: s("fr")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Language != "fr" {
				t.Errorf("language = %q", b.Language)
			}
		}},
		{"authors", model.Edits{Authors: &authors}, func(t *testing.T, b bookmodel.Bib) {
			if len(b.Authors) != 2 || b.Authors[0].Name != "Ann Rand" || b.Authors[1].Name != "Bo Li" {
				t.Fatalf("authors = %+v", b.Authors)
			}
			if b.Authors[0].SortName != "Rand, Ann" || b.Authors[1].SortName != "" {
				t.Errorf("sort names = %q, %q", b.Authors[0].SortName, b.Authors[1].SortName)
			}
		}},
		{"series rename keeps position", model.Edits{Series: s("The Quartet")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Series == nil || b.Series.Name != "The Quartet" || b.Series.Index != "2" {
				t.Errorf("series = %+v, want The Quartet at 2", b.Series)
			}
		}},
		{"series index keeps name", model.Edits{SeriesIndex: s("4")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Series == nil || b.Series.Name != "The Trilogy" || b.Series.Index != "4" {
				t.Errorf("series = %+v, want The Trilogy at 4", b.Series)
			}
		}},
		{"series cleared", model.Edits{Series: s("")}, func(t *testing.T, b bookmodel.Bib) {
			if b.Series != nil {
				t.Errorf("series = %+v, want nil", b.Series)
			}
		}},
	}

	for _, c := range corpora() {
		for _, tc := range cases {
			if tc.e.SortTitle != nil && !c.sortTitle {
				continue // no mechanism in this package version
			}
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				path := buildEpub(t, c.opf)
				bib, err := epub.Rewrite(path, book(t, path), tc.e)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, bib)

				// Rewrite returns the re-parsed Bib, so agreeing with a fresh
				// Parse of the file is the claim that the write landed on disk.
				fresh, err := epub.Parse(path)
				if err != nil {
					t.Fatal(err)
				}
				tc.check(t, *fresh)
			})
		}
	}
}

// TestRewriteIsIdempotent applies the same edit twice. The second write must
// produce byte-identical OPF: anything else means the writer appends where it
// should replace, and repeated edits would grow the file or reorder it forever.
//
// synctest freezes the clock so dcterms:modified cannot differ between the two
// writes for the trivial reason that a wall-clock second elapsed between them,
// which is a property of the machine and not of the writer. That the stamp is
// left alone by an edit changing nothing is its own rule, pinned by
// TestModifiedStampIsWrittenOnlyForARealChange.
func TestRewriteIsIdempotent(t *testing.T) {
	for _, c := range corpora() {
		for _, tc := range preservingEdits() {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					path := buildEpub(t, c.opf)
					if _, err := epub.Rewrite(path, book(t, path), tc.e); err != nil {
						t.Fatal(err)
					}
					once := readEntry(t, path, opfPath)

					if _, err := epub.Rewrite(path, book(t, path), tc.e); err != nil {
						t.Fatal(err)
					}
					twice := readEntry(t, path, opfPath)

					if !bytes.Equal(once, twice) {
						t.Errorf("second identical edit changed the file\n--- after one ---\n%s\n--- after two ---\n%s", once, twice)
					}
				})
			})
		}
	}
}

// TestRewriteIsIdempotentOnARebindingDocument is the corpus test's blind spot:
// no fixture above rebinds a prefix, so none takes a second edit on a document
// where our property is spelled differently from how we would write it fresh.
//
// sameProperty on the read side is what keeps it stable. The first edit declares
// dcterms2; every later one must recognise that element as ours or mint
// dcterms3, dcterms4 — growing the package element once per save. The foreign
// property is checked each pass for the converse: recognising ours must not
// start meaning we recognise theirs.
//
// spell's reuse branch never runs here (the element is found, not created) and
// is covered in opf/vocab_test.go.
func TestRewriteIsIdempotentOnARebindingDocument(t *testing.T) {
	opf := strings.Replace(epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="dcterms:modified">not-a-date</meta>`),
		`version="3.0"`, `version="3.0" prefix="dcterms: http://example.com/vocab#"`, 1)

	path := buildEpub(t, opf)
	var first string
	for i, title := range []string{"One", "Two", "Three"} {
		if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &title}); err != nil {
			t.Fatalf("edit %d: %v", i+1, err)
		}

		md := metadata(t, path)
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

// --- packages carrying both series encodings ---------------------------------
//
// A version="2.0" package may carry a belongs-to-collection meta (OPF 2.0
// §2.2.10 lets <meta> carry anything), and the reader prefers the EPUB 3
// collection whatever the version says — so a file can hold the series twice, in
// two encodings that disagree.
//
// The rule: write every encoding the file already uses, plus the one its version
// implies. Neither test below lets the two drift apart.

func TestEPUB2SeriesEditUpdatesBothEncodings(t *testing.T) {
	path := buildEpub(t, epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator opf:role="aut">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">The Old Series</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">2</meta>`))

	// The collection outranks the calibre metas on read, v2 package or not.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Old Series" {
		t.Fatalf("series before the edit = %+v, want The Old Series", bib.Series)
	}

	want := "The New Series"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	assertSeriesEncodings(t, path, want, "2")
}

// TestEPUB3SeriesEditUpdatesStaleCalibreMetas is the mirror: a v3 package
// carrying calibre metas has them brought into step, rather than left asserting
// a series the collection no longer names. A calibre reader consults them first.
func TestEPUB3SeriesEditUpdatesStaleCalibreMetas(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">The Old Series</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">2</meta>
    <meta name="calibre:series" content="The Old Series"/>
    <meta name="calibre:series_index" content="2"/>`))

	want := "The New Series"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	assertSeriesEncodings(t, path, want, "2")
}

// TestEPUB3SeriesEditDoesNotInjectCalibreMetas: a file that never carried
// the proprietary encoding does not acquire it. "Keep every encoding in step" is
// not licence to add one.
func TestEPUB3SeriesEditDoesNotInjectCalibreMetas(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">The Old Series</meta>
    <meta refines="#c01" property="collection-type">series</meta>`))

	want := "The New Series"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	if got := metadata(t, path).FindElement("//meta[@name='calibre:series']"); got != nil {
		t.Errorf("calibre:series = %q, want none — the file never carried that encoding",
			got.SelectAttrValue("content", ""))
	}
}

// assertSeriesEncodings checks that every encoding present in the document
// names the same series, and that exactly one collection survives.
func assertSeriesEncodings(t *testing.T, path, want, wantIndex string) {
	t.Helper()
	md := metadata(t, path)

	var collections []string
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" {
			collections = append(collections, m.Text())
		}
	}
	if !slices.Equal(collections, []string{want}) {
		t.Errorf("collections = %v, want exactly [%s]", collections, want)
	}
	if got := md.FindElement("//meta[@property='group-position']"); got == nil || got.Text() != wantIndex {
		t.Errorf("group-position = %v, want %q carried over", got, wantIndex)
	}
	if got := md.FindElement("//meta[@name='calibre:series']"); got == nil ||
		got.SelectAttrValue("content", "") != want {
		t.Errorf("calibre:series = %v, want %q in step with the collection", got, want)
	}
	if got := md.FindElement("//meta[@name='calibre:series_index']"); got == nil ||
		got.SelectAttrValue("content", "") != wantIndex {
		t.Errorf("calibre:series_index = %v, want %q", got, wantIndex)
	}

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != want || bib.Series.Index != wantIndex {
		t.Errorf("series = %+v, want %q at %s", bib.Series, want, wantIndex)
	}
}

// TestMultiLevelPositionNarrowsForEPUB2 pins the one place a multi-level
// position cannot survive: EPUB 2 has no group-position, so the series goes
// into calibre:series_index, which is a float by calibre's own convention.
// Writing the first two levels is the closest a calibre reader can act on, and
// it is a deliberate narrowing rather than a silent collapse to 1.
func TestMultiLevelPositionNarrowsForEPUB2(t *testing.T) {
	opf := epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>An Article</dc:title>
    <dc:creator opf:role="aut">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta name="calibre:series" content="Physical Review D"/>
    <meta name="calibre:series_index" content="1"/>`)

	path := buildEpub(t, opf)
	index := "2.2.1"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{SeriesIndex: &index}); err != nil {
		t.Fatal(err)
	}
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "2.2" {
		t.Errorf("series = %+v, want the position narrowed to 2.2 by the EPUB 2 encoding", bib.Series)
	}
}

// --- repeatable Dublin Core elements -----------------------------------------
//
// EPUB 3.3 §5.5.3.2.1 makes the optional DCMES elements "OPTIONAL child of
// metadata. Repeatable." The spec is silent on which to present. Every other
// repeatable field we read is first-wins (title §5.5.3.1.2, creator order
// §5.5.3.2.3, language §5.5.3.1.3) and calibre takes the first, but
// opfMetadata.Description is a plain string so encoding/xml keeps the last.
// That is emergent, not chosen — this test proposes making it consistent.

func TestFirstDescriptionWins(t *testing.T) {

	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:description>FIRST description.</dc:description>
    <dc:description>SECOND description.</dc:description>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != "FIRST description." {
		t.Errorf("description = %q, want the first, consistent with every other repeatable field", bib.Description)
	}
}

// TestDanglingCoverMetaFallsThrough: a legacy cover meta naming an id that
// is not in the manifest currently suppresses the id-heuristic fallback, so a
// *broken* pointer does worse than a missing one. The spec says nothing here;
// calibre keeps looking.
func TestDanglingCoverMetaFallsThrough(t *testing.T) {

	opf := epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta name="cover" content="does-not-exist"/>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath == "" {
		t.Error("no cover found; a stale cover meta should not suppress the fallback")
	}
}

// TestSeriesWithNoPositionDropsTheCalibreIndex covers a series whose collection
// carries no group-position, in a file that still has a calibre:series_index
// from some earlier tool. The position the edit carries over is empty, so the
// stale index goes rather than staying behind to contradict the collection.
func TestSeriesWithNoPositionDropsTheCalibreIndex(t *testing.T) {
	path := buildEpub(t, epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator opf:role="aut">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">The Old Series</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta name="calibre:series" content="The Old Series"/>
    <meta name="calibre:series_index" content="7"/>`))

	want := "The New Series"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	if el := md.FindElement("meta[@name='calibre:series']"); el == nil || el.SelectAttrValue("content", "") != want {
		t.Errorf("calibre:series = %v, want %q", el, want)
	}
	if el := md.FindElement("meta[@name='calibre:series_index']"); el != nil {
		t.Errorf("calibre:series_index = %q, want it gone with the position", el.SelectAttrValue("content", ""))
	}
}

// --- an EPUB 2 creator can lose its sort name --------------------------------

// TestEPUB2CreatorLosesAStaleSortName covers editing an author whose name is
// unchanged but whose sort name is gone. The creator element is reused, so the
// opf:file-as it was written with has to be removed rather than left behind
// describing a sort order the caller just cleared. The EPUB 3 half of this is
// TestSetAuthorsReuseBookkeeping in the internal tests.
func TestEPUB2CreatorLosesAStaleSortName(t *testing.T) {
	opf := epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator opf:role="aut" opf:file-as="Doe, Jane">Jane Doe</dc:creator>
    <dc:language>en</dc:language>`)

	path := buildEpub(t, opf)
	authors := []model.Author{{Name: "Jane Doe"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	if got := metadata(t, path).FindElement("creator").SelectAttrValue("opf:file-as", ""); got != "" {
		t.Errorf("opf:file-as = %q, want removed with the sort name", got)
	}
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "" {
		t.Errorf("authors = %+v, want one author with no sort name", bib.Authors)
	}
}

// TestEPUB3CreatorWithALegacySortNameTakesTheEdit covers an EPUB 3 package whose
// creators still carry the EPUB 2 opf:file-as — common in v2 files upgraded in
// place. Carrying both is allowed and no spec says which wins; disagreeing with
// itself is not.
//
// The read prefers the attribute, the v3 write only touches the refinement, so
// the edit lands where the read never looks and the stale value is reported
// forever. titleField.set guards the same hazard for the title. The EPUB 2 half
// is TestEPUB2CreatorLosesAStaleSortName above.
func TestEPUB3CreatorWithALegacySortNameTakesTheEdit(t *testing.T) {
	opf := strings.Replace(epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1" opf:file-as="Stale, Name">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <dc:language>en</dc:language>`),
		`xmlns:dc="http://purl.org/dc/elements/1.1/"`,
		`xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:opf="http://www.idpf.org/2007/opf"`, 1)

	path := buildEpub(t, opf)
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want the sort name the edit asked for", bib.Authors)
	}
	// Whichever mechanism the writer picks, the file must not end up claiming
	// both. A stale attribute left beside a fresh refinement is the failure.
	c := metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "" && got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want it updated or removed, not left stale", got)
	}
}

// TestUnprefixedFileAsIsUpdatedNotDuplicated covers a creator carrying a bare
// file-as rather than opf:file-as. Reading matches on the local name, so such an
// attribute is found and reported as the sort name; writing always qualifies
// with the opf prefix, so the update would land on a second attribute and leave
// the one the read prefers untouched.
//
// The element then asserts two sort names and the next read takes the stale one,
// which is the same silent no-op TestEPUB3CreatorWithALegacySortNameTakesTheEdit
// exists to prevent, one namespace lower.
func TestUnprefixedFileAsIsUpdatedNotDuplicated(t *testing.T) {
	opf := epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator opf:role="aut" file-as="Stale, Name">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`)

	path := buildEpub(t, opf)
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want the sort name the edit asked for", bib.Authors)
	}

	c := metadata(t, path).FindElement("creator")
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

// --- recovering from an empty value ------------------------------------------
//
// §5.5.2 makes an empty dc:title, creator or date invalid, and there the spec
// stops — what a reader does with one is unstated, so recovering rather than
// rejecting is ours, which is why these are not with the conformance assertions.
//
// Skip the empty value, use the next usable one. Where nothing usable remains
// the book still fails loudly.

// TestEmptyDateIsSkipped completes the family. An empty
// dc:date is invalid, and skipping it matters more than skipping an empty title
// or creator does: pubdate returns a date only when exactly one untagged
// dc:date carries a value, so counting an empty one would make it two and leave
// the book with no publication date at all rather than the one it has.
func TestEmptyDateIsSkipped(t *testing.T) {
	bib, err := epub.Parse(buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:date>   </dc:date>
    <dc:date>2020-01-02</dc:date>`)))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Pubdate != "2020-01-02" {
		t.Errorf("pubdate = %q, want the one date that carries a value", bib.Pubdate)
	}
}

// TestEmptyCreatorIsSkippedNotFatal is the creator half of the same rule,
// and the pair of branches it covers is the whole argument for skipping rather
// than rejecting: a stray empty creator costs nothing, and a file with no
// readable author at all still fails loudly.
//
// Rejecting the first case would lose an entire book over an element carrying
// no information, which is the opposite of what the empty title above does.
func TestEmptyCreatorIsSkippedNotFatal(t *testing.T) {
	t.Run("skipped alongside a usable one", func(t *testing.T) {
		bib, err := epub.Parse(buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:creator id="c2">   </dc:creator>
    <dc:language>en</dc:language>`)))
		if err != nil {
			t.Fatalf("a usable author sits beside the empty creator: %v", err)
		}
		if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand"}) {
			t.Errorf("authors = %v, want [Ann Rand]", got)
		}
	})

	t.Run("no readable author is an error", func(t *testing.T) {
		_, err := epub.Parse(buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">   </dc:creator>
    <dc:language>en</dc:language>`)))
		if err == nil {
			t.Fatal("a book whose only creator is empty parsed anyway")
		}
		if !strings.Contains(err.Error(), "no authors") {
			t.Errorf("err = %v, want it to say there are no authors", err)
		}
	})
}

func TestEmptyFirstTitleFallsThrough(t *testing.T) {

	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>   </dc:title>
    <dc:title>The Real Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatalf("a usable title follows the empty one: %v", err)
	}
	if bib.Title != "The Real Title" {
		t.Errorf("title = %q", bib.Title)
	}
}

// --- an edit has to land somewhere -------------------------------------------

// TestEmptyElementIsWrittenInPlace covers a file whose only dc:description is
// empty. §5.5.2 requires non-empty values, so the file is malformed and no rule
// says where a write should go. The element that is already there is used,
// rather than a second one added beside it, so the file ends up with one
// description however many rewrites it sees.
func TestEmptyElementIsWrittenInPlace(t *testing.T) {
	opf := epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:description>   </dc:description>
    <dc:description></dc:description>`)

	path := buildEpub(t, opf)
	desc := "A new description."
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Description: &desc}); err != nil {
		t.Fatal(err)
	}

	els := metadata(t, path).SelectElements("description")
	if len(els) != 2 {
		t.Fatalf("description count = %d, want the two the file had", len(els))
	}
	if els[0].Text() != desc {
		t.Errorf("first description = %q, want the edit", els[0].Text())
	}
	// first skips the empty element the write landed in front of.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != desc {
		t.Errorf("description = %q, want the edit read back", bib.Description)
	}
}

// --- a repeated author name ---------------------------------------------------

// TestRewriteRefusesDuplicateAuthors covers an author list naming the same
// person twice. Edits.Validate rejects it, and Rewrite re-checks so an
// unvalidated Edits cannot reach the file: two creators of one name have no
// meaning in either spec, and reusing one element for both would silently
// collapse the list instead.
func TestRewriteRefusesDuplicateAuthors(t *testing.T) {
	path := buildEpub(t, richOPF3)
	before := readEntry(t, path, opfPath)

	authors := []model.Author{{Name: "Jane Doe"}, {Name: "Jane Doe"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err == nil {
		t.Fatal("a duplicated author was accepted")
	}
	if !bytes.Equal(before, readEntry(t, path, opfPath)) {
		t.Error("the epub was rewritten by a refused edit")
	}
}

// --- refinements need a target -----------------------------------------------

// TestUnrefinedMetaIsNotACreatorsSortName covers a file carrying a file-as meta
// with no refines attribute, next to a creator with no id. Both are missing the
// thing that would link them, so the meta refines the package as a whole
// (§5.3.6) and must not be read as that creator's sort name.
func TestUnrefinedMetaIsNotACreatorsSortName(t *testing.T) {
	opf := epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <meta property="file-as">Someone, Else</meta>
    <dc:language>en</dc:language>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "" {
		t.Errorf("authors = %+v, want no sort name", bib.Authors)
	}
}

// --- cover fallback ordering --------------------------------------------------

// TestCoverHeuristicTakesTheLastMatch pins the fallback for files that name no
// cover at all: no cover-image property, no <meta name="cover">, just manifest
// ids that happen to contain "cover". Neither spec describes this, and calibre
// takes the first such item where this takes the last, so the divergence is
// pinned rather than left to be discovered by a book showing the wrong cover.
func TestCoverHeuristicTakesTheLastMatch(t *testing.T) {
	const opf = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="cover-thumb" href="thumb.jpg" media-type="image/jpeg"/>
    <item id="cover.jpg" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath != "OEBPS/cover.jpg" {
		t.Errorf("cover = %q, want the last matching item (calibre would take thumb.jpg)", bib.CoverPath)
	}
}

// --- duplicate refinements ----------------------------------------------------

// TestDuplicateRefinementsKeepTheirDuplicates pins a known gap. D.3.6 gives
// file-as "Cardinality: zero or one", so a creator carrying two of them is a
// malformed file; the writer updates the first and leaves the second
// contradicting it, and the reader takes the first. Removing every refinement
// to append a replacement is the churn this package avoids everywhere else, so
// the duplicate is left alone until a file is seen where it matters. Delete
// this test when that changes.
func TestDuplicateRefinementsKeepTheirDuplicates(t *testing.T) {
	opf := epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="file-as">First, Ann</meta>
    <meta refines="#c1" property="file-as">Second, Ann</meta>
    <dc:language>en</dc:language>`)

	path := buildEpub(t, opf)
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	var got []string
	for _, m := range metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "file-as" {
			got = append(got, m.Text())
		}
	}
	if !slices.Equal(got, []string{"Rand, Ann", "Second, Ann"}) {
		t.Errorf("file-as refines = %v, want the first updated and the second left", got)
	}
	// The reader takes the same one the writer updated, so the duplicate is
	// inert rather than contradicting what the edit asked for.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("sort name = %q, want the refinement the write updated", bib.Authors[0].SortName)
	}
}

// --- identifiers -------------------------------------------------------------

// TestIdentifiersKeepTheirCurrentKeying pins a known-wrong behaviour, on
// purpose. Bib.Identifiers is keyed by the element's XML id rather than by its
// scheme, and the index persists that as identifiers.scheme under a uniqueness
// constraint. Changing the keying rewrites rows in every existing library, so
// it is a migration, not a parser fix — and the rewrite of this package must
// not do it as a side effect. This test fails if the rewrite changes it
// accidentally; delete it when the keying is changed deliberately, together
// with the migration that rewrites the existing rows.
func TestIdentifiersKeepTheirCurrentKeying(t *testing.T) {
	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:identifier id="isbn">9780123456789</dc:identifier>
    <meta refines="#isbn" property="identifier-type" scheme="onix:codelist5">15</meta>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if got := bib.Identifiers["pub-id"]; got != "urn:uuid:1234" {
		t.Errorf(`Identifiers["pub-id"] = %q; keying changed — needs a migration`, got)
	}
	if got := bib.Identifiers["isbn"]; got != "9780123456789" {
		t.Errorf(`Identifiers["isbn"] = %q; keying changed — needs a migration`, got)
	}
}

// --- xml:lang and dir --------------------------------------------------------

// TestMetadataDirectionalitySurvives pins that the language and direction a
// publisher put on a metadata element survive an edit to a different field.
// They are attributes of the element, so this holds only while elements are
// reused rather than rebuilt.
//
// Nothing in §5.3.1 or §5.3.7 requires a writer to preserve them; this is the
// same preservation rule the rest of this file pins, applied to attributes.
func TestMetadataDirectionalitySurvives(t *testing.T) {
	const opf = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id" xml:lang="en" dir="ltr">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title xml:lang="ar" dir="rtl">العنوان</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

	path := buildEpub(t, opf)
	desc := "A new description."
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Description: &desc}); err != nil {
		t.Fatal(err)
	}
	title := metadata(t, path).SelectElement("title")
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

// --- metadata we do not model ------------------------------------------------

// TestUnmodelledMetadataSurvives pins that Dublin Core elements ebookfs has
// no field for are still carried through an edit untouched. Not modelling
// something is not a licence to drop it.
func TestUnmodelledMetadataSurvives(t *testing.T) {
	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:subject>Science Fiction</dc:subject>
    <dc:subject>Space Opera</dc:subject>
    <dc:publisher>Acme Press</dc:publisher>
    <dc:rights>All rights reserved.</dc:rights>
    <dc:source>urn:isbn:9780000000000</dc:source>`)

	path := buildEpub(t, opf)
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)
	for _, tag := range []string{"publisher", "rights", "source"} {
		if el := md.SelectElement(tag); el == nil {
			t.Errorf("dc:%s was dropped", tag)
		}
	}
	if n := len(md.SelectElements("subject")); n != 2 {
		t.Errorf("dc:subject count = %d, want 2", n)
	}
}

// --- a collection-type we do not own ------------------------------------------

// TestSchemedCollectionTypeSurvivesASeriesEdit pins that a series edit leaves a
// schemed collection-type alone. Both sides resolve to the first *unschemed*
// refinement, since series/set are only defined "when no scheme is specified"
// (D.3.4), so a value from someone else's code list is left as it was.
//
// Ours, not conformance: D.3.4 gives collection-type cardinality "zero or one",
// so the two refinements this needs cannot occur in a conforming file. We handle
// it anyway. The corpus above never produces two, which is why it is here.
func TestSchemedCollectionTypeSurvivesASeriesEdit(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">The Trilogy</meta>
    <meta refines="#c01" property="collection-type" scheme="onix:codelist148">12</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">2</meta>`))

	want := "The Quartet"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}

	var schemed, unschemed []string
	for _, m := range metadata(t, path).SelectElements("meta") {
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

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != want || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want %q at 2", bib.Series, want)
	}
}

// --- renaming a series the reader cannot see ----------------------------------

// TestSeriesRenameDoesNotDuplicate covers the write-side consequence of a
// collection the reader fails to resolve: when the reader cannot see the
// existing collection, the writer cannot either, so a rename adds a second one
// instead of rewriting the first — and the new one carries no group-position,
// resetting the book's position to 1.
//
// The fixture is opfSpecStyleWhitespace from spec_ext_test.go, where the reason
// a reader might miss the collection is spelled out. What is pinned here is only
// our rule: one collection, position preserved across a rename.
func TestSeriesRenameDoesNotDuplicate(t *testing.T) {

	path := buildEpub(t, opfSpecStyleWhitespace)
	want := "Renamed"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)
	var collections int
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "belongs-to-collection" {
			collections++
		}
	}
	if collections != 1 {
		t.Errorf("belongs-to-collection count = %d, want 1 — the rename duplicated it", collections)
	}

	bib, err := epub.Parse(path)
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

const ncxOPF = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator opf:role="aut">Jane Doe</dc:creator>
    <dc:language>en</dc:language>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`

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
	return writeEpub(t, baseEntries(ncxOPF, entry{name: "OEBPS/toc.ncx", data: []byte(ncx)}))
}

// ncxTexts returns the <text> values under every element with the given tag, so
// a test can assert on what the NCX now says without walking etree itself.
func ncxTexts(t *testing.T, epubPath, tag string) []string {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, epubPath, "OEBPS/toc.ncx")); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	var out []string
	for _, el := range doc.FindElements("//" + tag) {
		out = append(out, strings.TrimSpace(el.SelectElement("text").Text()))
	}
	return out
}

func TestNCXDocTitleFollowsATitleEdit(t *testing.T) {
	path := buildNCXEpub(t, ncxWith(`  <docTitle><text>Original Title</text></docTitle>`))

	title := "New Title"
	if _, err := writeBib(path, model.Edits{Title: &title}); err != nil {
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

	authors := []model.Author{{Name: "Ann Rewrite"}, {Name: "Bo Second"}, {Name: "Cy Third"}}
	if _, err := writeBib(path, model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	want := []string{"Ann Rewrite", "Bo Second", "Cy Third"}
	if got := ncxTexts(t, path, "docAuthor"); !slices.Equal(got, want) {
		t.Errorf("docAuthor = %q, want %q", got, want)
	}

	// The added ones must land before <navMap>, which the NCX content model
	// puts after docAuthor: appending to <ncx> would have made the file invalid.
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, "OEBPS/toc.ncx")); err != nil {
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
	path := buildNCXEpub(t, ncxWith(`  <docTitle><text>Original Title</text></docTitle>`))

	authors := []model.Author{{Name: "Ann Rewrite"}}
	if _, err := writeBib(path, model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	if got := ncxTexts(t, path, "docAuthor"); len(got) != 0 {
		t.Errorf("docAuthor = %q, want none", got)
	}
}

// A book with no NCX at all is the EPUB 3 norm, and an edit must not fail over
// the manifest not declaring one.
func TestTitleEditWithoutAnNCX(t *testing.T) {
	path := buildEpub(t, opf3)

	title := "New Title"
	bib, err := writeBib(path, model.Edits{Title: &title})
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
	ncx := ncxWith(`  <docTitle><text>Original Title</text></docTitle>`)
	path := buildNCXEpub(t, ncx)

	series := "A Series"
	if _, err := writeBib(path, model.Edits{Series: &series}); err != nil {
		t.Fatal(err)
	}

	if got := string(readEntry(t, path, "OEBPS/toc.ncx")); got != ncx {
		t.Errorf("ncx was rewritten:\n%s", got)
	}
}

// An unreadable NCX does not fail the edit, and is left exactly as it was. The
// second case is malformed only in its nesting, which is the kind of document
// etree used to correct silently.
func TestUnreadableNCXDoesNotFailTheEdit(t *testing.T) {
	for _, tc := range []struct{ name, ncx string }{
		{"syntax error", "<ncx><docTitle<</ncx>"},
		{"mismatched end tag", `<ncx><docTitle><text>Original Title</text></docTitle>` +
			`<navMap><navPoint id="p1"><content src="chapter1.xhtml"/></navPoint></wrong></ncx>`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildNCXEpub(t, tc.ncx)

			title := "New Title"
			bib, err := writeBib(path, model.Edits{Title: &title})
			if err != nil {
				t.Fatal(err)
			}
			if bib.Title != title {
				t.Errorf("title = %q, want %q", bib.Title, title)
			}
			if got := string(readEntry(t, path, "OEBPS/toc.ncx")); got != tc.ncx {
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
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator>Jane Doe</dc:creator>
    <dc:language>en</dc:language>
    <dc:description><![CDATA[`+value+`]]></dc:description>`))

	title := "New Title"
	bib, err := writeBib(path, model.Edits{Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != value {
		t.Errorf("description = %q, want %q", bib.Description, value)
	}

	got := string(readEntry(t, path, opfPath))
	if !strings.Contains(got, "<![CDATA["+value+"]]>") {
		t.Errorf("the CDATA section was re-encoded:\n%s", got)
	}
}

// --- multipart titles --------------------------------------------------------
//
// The fixture is §5.5.3.1.2's own example. The second element is another segment
// of the same title, so replacing the title has to take it too.

var opfMultipartTitle = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>THE LORD OF THE RINGS</dc:title>
    <dc:title>Part One: The Fellowship of the Ring</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`)

func TestTitleEditTakesTheOtherSegments(t *testing.T) {
	path := buildEpub(t, opfMultipartTitle)

	want := "The Hobbit"
	bib, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want})
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != want {
		t.Errorf("title = %q, want %q", bib.Title, want)
	}

	els := metadata(t, path).SelectElements("title")
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
	path := buildEpub(t, opfMultipartTitle)

	sort := "Lord of the Rings, The"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{SortTitle: &sort}); err != nil {
		t.Fatal(err)
	}

	if els := metadata(t, path).SelectElements("title"); len(els) != 2 {
		t.Errorf("dc:title count = %d, want the two the file had", len(els))
	}
}

// The second edit finds nothing to drop, so it leaves the file alone.
func TestTitleEditIsIdempotentAcrossSegments(t *testing.T) {
	path := buildEpub(t, opfMultipartTitle)

	want := "The Hobbit"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	first := readEntry(t, path, opfPath)

	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	if second := readEntry(t, path, opfPath); !bytes.Equal(first, second) {
		t.Errorf("a repeated edit rewrote the package document:\n%s", second)
	}
}
