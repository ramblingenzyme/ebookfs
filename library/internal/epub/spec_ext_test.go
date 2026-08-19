// These tests are organised by the metadata vocabulary — EPUB 3.3 Appendix D
// (meta properties) and OPF 2.0 publication metadata — rather than by this
// package's functions. That is deliberate: the bugs this file was written to
// catch were invisible from inside the code, which is self-consistent, and only
// appeared when a file was read against the vocabulary.
//
// A TestSpec* name means the specs say so: the assertion is cited from specs/
// with a section number, and changing it means the package stops conforming.
// Tests without the prefix pin choices — a calibre convention, a divergence we
// took deliberately, a known-wrong behaviour held still until it can be changed
// on purpose. Those are ours to revisit; the TestSpec ones are not.
//
// https://www.w3.org/TR/epub-33/#app-meta-property-vocab
// https://idpf.org/epub/20/spec/OPF_2.0_final_spec.html
//
// Every test here passes. The file was written with twelve of them skipped,
// each naming the behaviour the package did not have yet, and the 2026-08-19
// rewrite of this package turned them all on; each was verified to
// fail against the pre-rewrite code first, because a test that passes against
// the code it was written to condemn was never testing anything.
//
// So they now serve the other purpose: pinning behaviour that is correct, so a
// later change cannot lose it quietly. Several pin rules that only became
// correct on 2026-08-19, and a few pin deliberate divergences that would
// otherwise look like bugs to whoever reads this next.
package epub_test

import (
	"bytes"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// --- dc:title selection -------------------------------------------------------
//
// EPUB 3.3 §5.5.3.1.2 (The dc:title element):
//   "The first dc:title element in document order is the main title of the EPUB
//    publication (i.e., the primary one reading systems present to users)."
//   "Previous versions of this specification recommended using the title-type and
//    display-seq properties to identify and format the segments of multipart
//    titles ... It is still possible to add these semantics, but they are also not
//    well supported."
// OPF 2.0 §2.2.1 (<title>):
//   "Determination of the most appropriate titles is not defined by this
//    specification ... In the absence of such an algorithm, conforming Reading
//    Systems should consider either the first title element or all the title
//    elements as the most appropriate."
//
// So first-in-document-order is the 3.3 rule and one of the two endorsed OPF 2.0
// fallbacks. calibre instead resolves title-type=main; we decline to follow it,
// deliberately. title-type (D.3.13) says nothing about selection.

var opfTitleTypes = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="t1">The Complete Trilogy</dc:title>
    <meta refines="#t1" property="title-type">collection</meta>
    <dc:title id="t2">The Fellowship</dc:title>
    <meta refines="#t2" property="title-type">main</meta>
    <meta refines="#t2" property="file-as">Fellowship, The</meta>
    <dc:title id="t3">Being the First Part</dc:title>
    <meta refines="#t3" property="title-type">subtitle</meta>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`)

// TestSpecFirstTitleWins pins §5.5.3.1.2 against a file whose title-type
// refinements contradict document order — the first title is labelled
// "collection" and a later one "main". Such a file is self-contradictory under
// 3.3, and we resolve it the way the spec says: first in document order, read
// and written. calibre would pick t2 — a deliberate divergence, not a bug.
// Revisit only if round-tripping with calibre becomes a goal.
func TestSpecFirstTitleWins(t *testing.T) {
	path := buildEpub(t, opfTitleTypes)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "The Complete Trilogy" {
		t.Errorf("title = %q, want the first dc:title per §5.5.3.1.2", bib.Title)
	}

	// The write side must target the same element the read side resolved, or the
	// edit would appear not to happen.
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)
	if got := titleByID(md, "t1"); got != want {
		t.Errorf("first title = %q, want %q — read and write must resolve the same element", got, want)
	}
	for _, id := range []string{"t2", "t3"} {
		if titleByID(md, id) == want {
			t.Errorf("title %s was overwritten; only the resolved title is ours", id)
		}
	}
}

// --- display-seq -------------------------------------------------------------
//
// EPUB 3.3 Appendix D.3.5 (display-seq):
//   "This property only applies where precedence rules have not already been
//    defined (e.g., precedence is given to creators based on their appearance in
//    document order)."
// EPUB 3.3 §5.5.3.2.3 (The dc:creator element):
//   "The document order of dc:creator elements in the metadata section determines
//    the display priority, where the first dc:creator element encountered is the
//    primary creator."
// OPF 2.0 §2.2.2 (<creator>):
//   "The order of creator elements is presumed to define the order in which the
//    creators' names should be presented by the Reading System."
//
// display-seq is therefore inert on creators in both specs: document order is
// already the precedence rule. A display-seq that disagrees with it changes
// nothing for a conforming reader, and neither should it for us.

var opfDisplaySeq = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c1" property="display-seq">2</meta>
    <dc:creator id="c2">Bo Li</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c2" property="display-seq">1</meta>
    <dc:language>en</dc:language>`)

// TestSpecCreatorOrderIsDocumentOrder pins that a contradicting display-seq does
// not reorder the authors. It is the inverse of a test written on 2026-08-19
// which asserted that display-seq *should* win; D.3.5 says it does not apply
// once a precedence rule exists, and for creators one does.
func TestSpecCreatorOrderIsDocumentOrder(t *testing.T) {
	path := buildEpub(t, opfDisplaySeq)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want document order [Ann Rand, Bo Li] per §5.5.3.2.3", got)
	}
}

// --- dcterms:modified --------------------------------------------------------

// TestSpecModifiedIsUpdated covers the inverse case: the one piece of
// metadata the writer should touch and does not.
//
// EPUB 3.3 §5.5.5 (Last modified date):
//
//	"The metadata section MUST contain exactly one dcterms:modified property
//	 containing the last modification date. The value of this property MUST be an
//	 [iso8601-1] complete representation of a date and time of day matching the
//	 extended format: YYYY-MM-DDThh:mm:ssZ"
//	"EPUB creators MUST express the last modification date in Coordinated
//	 Universal Time (UTC) and MUST terminate it with the "Z" (Zulu) time zone
//	 indicator."
//	"EPUB creators should update the last modified date whenever they make
//	 changes" — lowercase "should", so non-RFC2119 per §1.5. The format and
//	 cardinality, however, are MUST.
func TestSpecModifiedIsUpdated(t *testing.T) {

	path := buildEpub(t, richOPF3) // carries dcterms:modified 2020-01-02T00:00:00Z
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	got := metadata(t, path).FindElement("//meta[@property='dcterms:modified']")
	if got == nil {
		t.Fatal("dcterms:modified was removed")
	}
	if got.Text() == "2020-01-02T00:00:00Z" {
		t.Errorf("dcterms:modified = %q, want the time of this rewrite", got.Text())
	}
	// Format and cardinality are MUST, unlike the update itself.
	if n := len(metadata(t, path).FindElements("//meta[@property='dcterms:modified']")); n != 1 {
		t.Errorf("dcterms:modified count = %d, want exactly one per §5.5.5", n)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", got.Text()); err != nil {
		t.Errorf("dcterms:modified = %q, want the extended UTC format YYYY-MM-DDThh:mm:ssZ", got.Text())
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

// --- collection-type ---------------------------------------------------------
//
// EPUB 3.3 Appendix D.3.4 (collection-type) defines exactly two values when no
// scheme is given: "series — A sequence of related works that are formally
// identified as a group" and "set — A finite collection of works that together
// constitute a single intellectual unit". "publisher-series" in the fixture below
// is deliberately NOT a 3.3 value: it is the unrecognised-collection-type case,
// which must not be mistaken for the book's series.
//
// D.3.3 (belongs-to-collection) blesses the nesting the fixture uses: "It is also
// possible to chain these properties using the refines attribute to indicate that
// one collection is itself a member of another collection."

var opfCollections = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="ps1">Acme Classics</meta>
    <meta refines="#ps1" property="collection-type">publisher-series</meta>
    <meta property="belongs-to-collection" id="set1">Complete Works</meta>
    <meta refines="#set1" property="collection-type">set</meta>
    <meta property="belongs-to-collection" id="s1" refines="#set1">The Trilogy</meta>
    <meta refines="#s1" property="collection-type">series</meta>
    <meta refines="#s1" property="group-position">2</meta>`)

// TestSpecOnlySeriesCollectionIsTheSeries pins that neither a publisher-series
// nor a set is mistaken for the book's series, and that a series nested inside
// a set (expressed by the series meta refining the set) is renamed in place —
// keeping its nesting and leaving the parent alone. Correct as of 2026-08-19;
// the rewrite must keep it so.
func TestSpecOnlySeriesCollectionIsTheSeries(t *testing.T) {
	path := buildEpub(t, opfCollections)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Trilogy" || bib.Series.Index != "2" {
		t.Fatalf("series = %+v, want The Trilogy at 2", bib.Series)
	}

	want := "The Quartet"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)

	for _, c := range []struct{ id, text string }{
		{"ps1", "Acme Classics"},
		{"set1", "Complete Works"},
	} {
		el := md.FindElement("//meta[@id='" + c.id + "']")
		if el == nil || el.Text() != c.text {
			t.Errorf("collection %s = %v, want %q untouched", c.id, el, c.text)
		}
	}
	series := md.FindElement("//meta[@id='s1']")
	if series == nil {
		t.Fatal("the series collection was replaced rather than rewritten")
	}
	if series.Text() != want {
		t.Errorf("series = %q, want %q", series.Text(), want)
	}
	if got := series.SelectAttrValue("refines", ""); got != "#set1" {
		t.Errorf("series nesting = %q, want it still inside #set1", got)
	}
}

// --- role --------------------------------------------------------------------
//
// EPUB 3 carries the MARC relator as a role refine, EPUB 2 as opf:role. Neither
// spec states that an omitted role means "author": that is our reading of
// dc:creator as the party "responsible for the creation of the content"
// (3.3 §5.5.3.2.3) / "A primary creator or author of the publication"
// (OPF 2.0 §2.2.2). Pinned here as an interpretation, not a conformance claim.

func TestSpecOnlyAuthorRoleCreatorsAreAuthors(t *testing.T) {
	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <dc:creator id="c2">Acme Editorial Board</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">edt</meta>
    <dc:creator id="c3">Bo Li</dc:creator>
    <dc:language>en</dc:language>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	// c2 is an editor, so it is not an author; c3 has no role, which defaults
	// to author.
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want [Ann Rand, Bo Li]", got)
	}

	// An authors edit must not disturb the editor, who is not ours to rewrite.
	authors := []model.Author{{Name: "Ann Rand"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}
	if el := metadata(t, path).FindElement("//creator[@id='c2']"); el == nil || el.Text() != "Acme Editorial Board" {
		t.Error("the editor was removed by an authors edit")
	}
}

// --- xml:lang and dir --------------------------------------------------------

// TestSpecMetadataDirectionalitySurvives pins that the language and direction a
// publisher put on a metadata element survive an edit to a different field.
// They are attributes of the element, so this holds only while elements are
// reused rather than rebuilt.
func TestSpecMetadataDirectionalitySurvives(t *testing.T) {
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

// TestSpecUnmodelledMetadataSurvives pins that Dublin Core elements ebookfs has
// no field for are still carried through an edit untouched. Not modelling
// something is not a licence to drop it.
func TestSpecUnmodelledMetadataSurvives(t *testing.T) {
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

// --- multiple dc:language ----------------------------------------------------

// TestSpecFirstLanguageWins is spec-backed for EPUB 3, not merely our choice.
// §5.5.3.1.3: "Although EPUB creators MAY specify additional dc:language elements
// for multilingual Publications, reading systems will treat the first dc:language
// element in document order as the primary language of the EPUB publication."
// OPF 2.0 §2.2.12 permits multiple but defines no primary-selection rule, so for
// EPUB 2 first-wins genuinely is ours.
func TestSpecFirstLanguageWins(t *testing.T) {
	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:language>fr</dc:language>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Language != "en" {
		t.Errorf("language = %q, want the first of en, fr", bib.Language)
	}

	// Editing it rewrites the first and leaves the second alone.
	fr := "de"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Language: &fr}); err != nil {
		t.Fatal(err)
	}
	langs := metadata(t, path).SelectElements("language")
	if len(langs) != 2 {
		t.Fatalf("dc:language count = %d, want 2", len(langs))
	}
	if langs[0].Text() != "de" || langs[1].Text() != "fr" {
		t.Errorf("languages = %q, %q, want de, fr", langs[0].Text(), langs[1].Text())
	}
}

// TestSpecSchemedCollectionTypeSurvivesASeriesEdit is the write half of D.3.4.
// The reader resolves collection-type to the first *unschemed* refinement,
// because series/set are only defined "when no scheme is specified". The writer
// must resolve it the same way: a collection-type drawn from someone else's code
// list is metadata ebookfs does not own, and overwriting it with "series" is the
// same class of loss as the dropped creator refinements this suite exists for.
//
// It needs two collection-type refinements to show, which is why the corpus in
// rewrite_ext_test.go never caught it.
func TestSpecSchemedCollectionTypeSurvivesASeriesEdit(t *testing.T) {
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

// TestSpecSeriesCarryOverMatchesWhatTheReaderSees covers the rule that a
// partial edit carries the other half over from the file — and that "the file"
// means the same place a read would have taken it from.
//
// §5.5.2 requires non-empty values, so a belongs-to-collection with no name is
// invalid and the reader falls through to the calibre metas to find a usable
// series. A writer resolving the collection by its own rule instead sees the
// empty name, reads it as "this book has no series", and an index-only edit
// deletes a series the reader was reporting.
func TestSpecSeriesCarryOverMatchesWhatTheReaderSees(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01"></meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta name="calibre:series" content="The Trilogy"/>
    <meta name="calibre:series_index" content="3"/>`))

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Trilogy" {
		t.Fatalf("series before the edit = %+v, want The Trilogy from the calibre metas", bib.Series)
	}

	index := "5"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{SeriesIndex: &index}); err != nil {
		t.Fatal(err)
	}
	bib, err = epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil {
		t.Fatal("an index-only edit deleted the series the reader reported")
	}
	if bib.Series.Name != "The Trilogy" || bib.Series.Index != index {
		t.Errorf("series = %+v, want The Trilogy at %s", bib.Series, index)
	}
}

// --- id uniqueness -----------------------------------------------------------
//
// XML 1.0 §3.3.1 (Attribute Types):
//   "Values of type ID MUST match the Name production. A name MUST NOT appear
//    more than once in an XML document as a value of this type; i.e., ID values
//    MUST uniquely identify the elements which bear them."
//
// EPUB 3.3 §5.3.6 makes refines point at an element by fragment, so two elements
// answering to one id is not a cosmetic problem: a refinement would target both
// and epubcheck rejects the package.
//
// The writer mints ids at three sites — the title it attaches a file-as to, a
// new creator, and a new collection. Before 2026-08-19 each minted
// independently: "ebookfs-title" and "ebookfs-series" were assigned with no
// collision check at all, and the creator minter only scanned other creators,
// so it could not see a title or a collection already holding the name it was
// about to hand out. All three now go through opf.ensureID, which checks every
// id in the document.

func TestSpecMintedIDsDoNotCollide(t *testing.T) {
	// Each fixture already contains an element squatting on the id the writer
	// would otherwise mint, on a *different* kind of element than the one being
	// written — the case the old per-kind minter could not see.
	for _, tc := range []struct {
		name  string
		meta  string
		edits model.Edits
	}{
		{
			name: "title sort vs a squatted ebookfs-title",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="ebookfs-title">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edits: model.Edits{SortTitle: new("Title, The")},
		},
		{
			// Both spellings the minter has ever produced are squatted, on
			// elements that are not creators — which is precisely what the old
			// creator-only scan could not see.
			name: "new creator vs a squatted ebookfs-creator",
			meta: `    <dc:identifier id="ebookfs-creator-1">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-creator">The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edits: model.Edits{Authors: &[]model.Author{{Name: "Someone Else"}}},
		},
		{
			name: "new collection vs a squatted ebookfs-series",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-series">The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edits: model.Edits{Series: new("The Trilogy")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildEpub(t, epub3(tc.meta))
			if _, err := epub.Rewrite(path, book(t, path), tc.edits); err != nil {
				t.Fatal(err)
			}
			assertUniqueIDs(t, path)
		})
	}
}

// TestSpecRepeatedEditsDoNotCollideIDs is the same rule over time: the writer
// mints against a document that already contains ids it minted earlier, so
// growing the author list one edit at a time must not hand out a name twice.
func TestSpecRepeatedEditsDoNotCollideIDs(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <dc:language>en</dc:language>`))

	names := []string{"Ann Rand"}
	for _, add := range []string{"Bo Carr", "Cy Dunn", "Di Ekko"} {
		names = append(names, add)
		authors := make([]model.Author, len(names))
		for i, n := range names {
			authors[i] = model.Author{Name: n}
		}
		if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
			t.Fatalf("adding %s: %v", add, err)
		}
		assertUniqueIDs(t, path)

		bib, err := epub.Parse(path)
		if err != nil {
			t.Fatalf("after adding %s: %v", add, err)
		}
		if got := authorNames(bib); !slices.Equal(got, names) {
			t.Fatalf("authors = %v, want %v", got, names)
		}
	}
}

// TestSpecReorderingAuthorsKeepsIDsUnique covers the id-minting hazard that
// reordering creates. A new author placed *before* an existing one is minted an
// id while that existing creator has not yet been re-added, so any minting rule
// that only looks at creators currently in the tree cannot see the id it is
// about to duplicate — and duplicate ids cross-wire the refinements, silently
// swapping one author's sort name for another's.
func TestSpecReorderingAuthorsKeepsIDsUnique(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="ebookfs-creator">Alice</dc:creator>
    <meta refines="#ebookfs-creator" property="file-as">Alice, A</meta>
    <dc:language>en</dc:language>`))

	authors := []model.Author{{Name: "Bob", SortName: "Bob, B"}, {Name: "Alice", SortName: "Alice, A"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}
	assertUniqueIDs(t, path)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Bob", "Alice"}) {
		t.Fatalf("authors = %v, want [Bob Alice] in the order given", got)
	}
	for i, want := range []string{"Bob, B", "Alice, A"} {
		if got := bib.Authors[i].SortName; got != want {
			t.Errorf("%s sort name = %q, want %q", bib.Authors[i].Name, got, want)
		}
	}
}

// assertUniqueIDs checks the whole package document, not just <metadata>: a
// minted id can collide with a manifest item's just as easily.
func assertUniqueIDs(t *testing.T, path string) {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, opfPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	seen := map[string]bool{}
	for _, el := range doc.FindElements("//*[@id]") {
		id := el.SelectAttrValue("id", "")
		if seen[id] {
			t.Errorf("id %q appears on more than one element; XML 1.0 §3.3.1 requires ID values to be unique", id)
		}
		seen[id] = true
	}
}

// --- packages carrying both series encodings ---------------------------------
//
// Nothing forbids a version="2.0" package from containing a
// belongs-to-collection meta — OPF 2.0 §2.2.10 lets <meta> carry anything — and
// the reader prefers the EPUB 3 collection over the proprietary calibre metas
// whatever the package version says. A file can therefore hold the series twice
// over, in two encodings that disagree.
//
// The rule is to write every encoding the file already uses, plus the one its
// version implies: an edit no reader can miss, and nothing the file's author put
// there is deleted. Neither test below lets the two encodings drift apart.

func TestSpecEPUB2SeriesEditUpdatesBothEncodings(t *testing.T) {
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

// --- helpers -----------------------------------------------------------------
func metadata(t *testing.T, path string) *etree.Element {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, opfPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	md := doc.FindElement("//metadata")
	if md == nil {
		t.Fatal("result has no <metadata>")
	}
	return md
}

func titleByID(md *etree.Element, id string) string {
	for _, el := range md.SelectElements("title") {
		if el.SelectAttrValue("id", "") == id {
			return el.Text()
		}
	}
	return ""
}

func authorNames(b *model.Bib) []string {
	var out []string
	for _, a := range b.Authors {
		out = append(out, a.Name)
	}
	return out
}

// --- whitespace in metadata values -------------------------------------------
//
// EPUB 3.3 §5.5.2 (Metadata values):
//   "These elements MUST have non-empty values after leading and trailing ASCII
//    whitespace [infra] is stripped ... Whitespace within these element values is
//    not significant. Sequences of one or more whitespace characters are
//    collapsed to a single space [infra] during processing."
//
// Collapsing is a processing step, not an authoring rule, so a conforming reader
// must do it. Every normative example in §5.5 and Appendix D pretty-prints the
// value on its own line — Example 24 is a role refine written exactly like the
// one below.

var opfSpecStyleWhitespace = epub3(`    <dc:identifier id="pub-id">
      urn:uuid:A1B0D67E
    </dc:identifier>
    <dc:title id="t1">Norwegian Wood</dc:title>
    <meta refines="#t1" property="file-as">
      Norwegian Wood
    </meta>
    <dc:creator id="creator">Haruki Murakami</dc:creator>
    <meta refines="#creator" property="role" scheme="marc:relators" id="role">
      aut
    </meta>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">
      The New French Cuisine Masters
    </meta>
    <meta refines="#c01" property="collection-type">
      series
    </meta>
    <meta refines="#c01" property="group-position">
      2
    </meta>`)

// TestSpecWhitespaceIsCollapsed is the highest-value test in this file: the
// package currently fails to parse this document at all ("no authors"), because
// the role refine reads as "\n      aut\n    ". The same raw comparison drops the
// series and stores a SortTitle with leading newlines, which becomes the key for
// the default list order.
func TestSpecWhitespaceIsCollapsed(t *testing.T) {

	bib, err := epub.Parse(buildEpub(t, opfSpecStyleWhitespace))
	if err != nil {
		t.Fatalf("a document formatted the way the spec prints its own examples must parse: %v", err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Haruki Murakami"}) {
		t.Errorf("authors = %v, want [Haruki Murakami]", got)
	}
	if bib.SortTitle != "Norwegian Wood" {
		t.Errorf("sort title = %q, want it collapsed and trimmed", bib.SortTitle)
	}
	if bib.Series == nil || bib.Series.Name != "The New French Cuisine Masters" || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want The New French Cuisine Masters at 2", bib.Series)
	}
	if got := bib.Identifiers["pub-id"]; got != "urn:uuid:A1B0D67E" {
		t.Errorf("identifier = %q, want it collapsed and trimmed", got)
	}
}

// TestSpecWhitespaceInEPUB2RoleAttribute is the same failure through the EPUB 2
// attribute path. XML 1.0 §3.3.3 attribute-value normalization turns a newline
// inside an attribute value into a space but does not trim it, so a wrapped
// opf:role arrives padded.
func TestSpecWhitespaceInEPUB2RoleAttribute(t *testing.T) {

	opf := epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Alice in Wonderland</dc:title>
    <dc:creator opf:role=" aut " opf:file-as="Carroll, Lewis">Lewis Carroll</dc:creator>
    <dc:language>en</dc:language>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatalf("a padded opf:role must still be an author role: %v", err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll]", got)
	}
}

// --- dc-metadata / x-metadata ------------------------------------------------
//
// OPF 2.0 §2.2 (Publication Metadata):
//   "Reading Systems must allow the specification of the deprecated dc-metadata
//    and x-metadata elements. ... If the dc-metadata element is used, all dc
//    elements must go in dc-metadata and all other metadata elements, if any,
//    must go into x-metadata."

const opfWrappers = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="pub-id">
  <metadata>
    <dc-metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
      <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
      <dc:title>Alice in Wonderland</dc:title>
      <dc:creator opf:role="aut" opf:file-as="Carroll, Lewis">Lewis Carroll</dc:creator>
      <dc:language>en</dc:language>
    </dc-metadata>
    <x-metadata>
      <meta name="calibre:series" content="Alice"/>
      <meta name="cover" content="cover-img"/>
    </x-metadata>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`

func TestSpecLegacyMetadataWrappers(t *testing.T) {
	bib, err := epub.Parse(buildEpub(t, opfWrappers))
	if err != nil {
		t.Fatalf("reading systems MUST allow dc-metadata/x-metadata: %v", err)
	}
	if bib.Title != "Alice in Wonderland" {
		t.Errorf("title = %q", bib.Title)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v", got)
	}
	if bib.Series == nil || bib.Series.Name != "Alice" {
		t.Errorf("series = %+v, want Alice from x-metadata", bib.Series)
	}
}

// --- role, cardinality zero or more ------------------------------------------
//
// EPUB 3.3 Appendix D.3.10 (role): Cardinality "zero or more".
//   "When attaching multiple roles to an individual or organization, the
//    importance of the roles should match the document order of their containing
//    meta elements (i.e., the first meta element encountered should contain the
//    most important role)."
// Example 92 gives one dc:creator both aut and ill — so illustrator-first is the
// spec's own encoding for an illustrator who also wrote the book.

func TestSpecMultipleRoleRefines(t *testing.T) {

	opf := func(first, second string) string {
		return epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>Where the Wild Things Are</dc:title>
    <dc:creator id="creator01">Maurice Sendak</dc:creator>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + first + `</meta>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + second + `</meta>
    <dc:language>en</dc:language>`)
	}

	for _, order := range [][2]string{{"ill", "aut"}, {"aut", "ill"}} {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			path := buildEpub(t, opf(order[0], order[1]))
			bib, err := epub.Parse(path)
			if err != nil {
				t.Fatalf("a creator with several roles including aut is an author: %v", err)
			}
			if got := authorNames(bib); !slices.Equal(got, []string{"Maurice Sendak"}) {
				t.Fatalf("authors = %v, want [Maurice Sendak]", got)
			}

			// A no-op author edit must not strip the role that is not ours.
			authors := []model.Author{{Name: "Maurice Sendak"}}
			if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
				t.Fatal(err)
			}
			md := metadata(t, path)
			var roles []string
			for _, m := range md.SelectElements("meta") {
				if m.SelectAttrValue("property", "") == "role" {
					roles = append(roles, m.Text())
				}
			}
			if !slices.Contains(roles, "ill") {
				t.Errorf("roles after a no-op edit = %v, want the ill credit kept", roles)
			}
		})
	}
}

// --- refines is a URL, not always "#id" --------------------------------------
//
// EPUB 3.3 §5.3.6 (The refines attribute):
//   "EPUB creators MUST use as the value a path-relative-scheme-less-URL string,
//    optionally followed by U+0023 (#) and a URL-fragment string"
// so refines="content.opf#creator01" is conformant.

func TestSpecPathQualifiedRefines(t *testing.T) {

	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="t1">The Title</dc:title>
    <meta refines="content.opf#t1" property="file-as">Title, The</meta>
    <dc:creator id="creator01">Lewis Carroll</dc:creator>
    <meta refines="content.opf#creator01" property="role" scheme="marc:relators">aut</meta>
    <meta refines="content.opf#creator01" property="file-as">Carroll, Lewis</meta>
    <dc:creator id="creator02">Sir John Tenniel</dc:creator>
    <meta refines="content.opf#creator02" property="role" scheme="marc:relators">ill</meta>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">Alice</meta>
    <meta refines="content.opf#c01" property="collection-type">series</meta>
    <meta refines="content.opf#c01" property="group-position">2</meta>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.SortTitle != "Title, The" {
		t.Errorf("sort title = %q, want it resolved through a path-qualified refines", bib.SortTitle)
	}
	// The illustrator must not be promoted to author by an unresolvable role.
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll] only", got)
	}
	if len(bib.Authors) > 0 && bib.Authors[0].SortName != "Carroll, Lewis" {
		t.Errorf("author sort name = %q", bib.Authors[0].SortName)
	}
	if bib.Series == nil || bib.Series.Name != "Alice" || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want Alice at 2", bib.Series)
	}

	// D.3.6 file-as: "Cardinality: zero or one". An edit must not add a second
	// one beside the refine it failed to match.
	sort := "New, The"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{SortTitle: &sort}); err != nil {
		t.Fatal(err)
	}
	var fileAs int
	for _, m := range metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "file-as" &&
			strings.Contains(m.SelectAttrValue("refines", ""), "t1") {
			fileAs++
		}
	}
	if fileAs != 1 {
		t.Errorf("file-as refines on the title = %d, want exactly one per D.3.6", fileAs)
	}
}

// TestSpecSeriesRenameDoesNotDuplicate covers the write-side consequence: when
// the reader cannot see the existing collection, the writer cannot either, so a
// rename adds a second one instead of rewriting the first — and the new one
// carries no group-position, resetting the book's position to 1.
func TestSpecSeriesRenameDoesNotDuplicate(t *testing.T) {

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

// --- group-position ----------------------------------------------------------
//
// EPUB 3.3 Appendix D.3.7 (group-position):
//   Allowed value(s): "A single xsd:unsignedInt or series of decimal-separated
//   numbers (e.g., 1 or 2.2.1)."
// Example 89 notes 98.4 means volume 98, issue 4 — not the number 98.4. No
// numeric type holds either, so SeriesRef.Index is the string as written.

func TestSpecGroupPositionMultiLevel(t *testing.T) {

	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>An Article</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">Physical Review D</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">2.2.1</meta>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil {
		t.Fatal("series missing")
	}
	if bib.Series.Index != "2.2.1" {
		t.Errorf("index = %q, want the multi-level position kept exactly as written", bib.Series.Index)
	}
}

// TestSpecGroupPositionLevelsAreNotDecimals: D.3.7 counts levels, so "1.10" is
// volume 1, issue 10 — not the number 1.1. calibre:series_index is a float and
// needs its trailing zeros dropped ("1.0" means "1"); group-position is never
// calibre-written and must not get the same treatment.
func TestSpecGroupPositionLevelsAreNotDecimals(t *testing.T) {
	epub3Index := func(pos string) string {
		return epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>An Article</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">Physical Review D</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">` + pos + `</meta>`)
	}

	bib, err := epub.Parse(buildEpub(t, epub3Index("1.10")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.10" {
		t.Errorf("index = %+v, want 1.10 — issue 10 of volume 1, not 1.1", bib.Series)
	}

	// And it stays distinguishable from a genuine 1.1.
	bib, err = epub.Parse(buildEpub(t, epub3Index("1.1")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.1" {
		t.Errorf("index = %+v, want 1.1", bib.Series)
	}
}

// TestSpecGroupPositionMultiLevelRoundTrips is the write half: a multi-level
// position must survive being set, not just being read. Writing it through a
// float was what silently collapsed it.
func TestSpecGroupPositionMultiLevelRoundTrips(t *testing.T) {
	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>An Article</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">Physical Review D</meta>
    <meta refines="#c01" property="collection-type">series</meta>
    <meta refines="#c01" property="group-position">1</meta>`)

	path := buildEpub(t, opf)
	want := "2.2.1" // no float holds this, which is the point
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{SeriesIndex: &want}); err != nil {
		t.Fatal(err)
	}
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != want {
		t.Errorf("series = %+v, want the position written back as %q", bib.Series, want)
	}
	if got := metadata(t, path).FindElement("//meta[@property='group-position']"); got == nil || got.Text() != want {
		t.Errorf("group-position element = %v, want %q verbatim", got, want)
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

// --- empty values ------------------------------------------------------------
//
// §5.5.2 requires non-empty values after trimming, so a file with an empty
// dc:title is invalid; the spec does not say what to do with one. translateLanguage
// already skips empties to find a usable value, and doing the same for the title
// recovers a book whose real title is right there.

func TestSpecEmptyFirstTitleFallsThrough(t *testing.T) {

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

// --- schemed collection-type -------------------------------------------------
//
// EPUB 3.3 Appendix D.3.4 (collection-type):
//   "When the collection-type value is drawn from a code list or other formal
//    enumeration, EPUB creators SHOULD attach a scheme attribute to identify its
//    source. This specification also defines the following collection types when
//    no scheme is specified: series / set."
// So "series" under someone else's scheme is not the EPUB series type.

func TestSpecSchemedCollectionTypeIsNotOurSeries(t *testing.T) {

	var opf = epub3(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta property="belongs-to-collection" id="c01">Acme Bundle</meta>
    <meta refines="#c01" property="collection-type" scheme="onix:codelist148">series</meta>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series != nil {
		t.Errorf("series = %+v, want nil — the collection-type is drawn from another scheme", bib.Series)
	}
}

// --- cover resolution --------------------------------------------------------
//
// EPUB 3.3 §5.9.3 (The meta element):
//   "The [opf-201] meta element also allows EPUB creators to identify a cover
//    image for EPUB 2 reading systems. In EPUB 3, the cover image must be
//    identified using the cover-image property on the manifest item for the
//    image."

// TestSpecCoverImagePropertyBeatsLegacyMeta pins behaviour that is already
// correct but stated only by the order of two loops in translateCover: reorder
// them in a rewrite and the result flips with nothing failing.
func TestSpecCoverImagePropertyBeatsLegacyMeta(t *testing.T) {
	const opf = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <meta name="cover" content="legacy-cover"/>
  </metadata>
  <manifest>
    <item id="legacy-cover" href="old.jpg" media-type="image/jpeg"/>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath != "OEBPS/cover.jpg" {
		t.Errorf("cover = %q, want the cover-image manifest item to win per §5.9.3", bib.CoverPath)
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

// --- dc:date events ----------------------------------------------------------
//
// OPF 2.0 §2.2.7 (<date>):
//   "The date element has one optional OPF event attribute. The set of values for
//    event are not defined by this specification; possible values may include:
//    creation, publication, and modification."
//
// The vocabulary is open, and translateDate recognises only the literal
// "publication". This test pins the resulting closed-world behaviour, which the
// code documents deliberately. Pinned so a rewrite changes it on purpose rather
// than by accident.

func TestSpecUnrecognisedDateEventsLeaveNoPubdate(t *testing.T) {
	var opf = epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>
    <dc:date opf:event="creation">1999-01-01</dc:date>
    <dc:date opf:event="original-publication">2000-01-01</dc:date>
    <dc:date opf:event="modification">2001-01-01</dc:date>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Pubdate != "" {
		t.Errorf("pubdate = %q; the closed-world reading changed — decide it deliberately", bib.Pubdate)
	}
}

// --- writing into the legacy wrappers ----------------------------------------
//
// OPF 2.0 §2.2 (Publication Metadata):
//   "If the dc-metadata element is used, all dc elements must go in dc-metadata
//    and all other metadata elements, if any, must go into x-metadata."
//
// TestSpecLegacyMetadataWrappers pins the read side. This pins the write side:
// the MUST binds anything ebookfs adds to such a file just as much as what it
// found there, so a new dc:description belongs in dc-metadata and a new
// calibre:series_index in x-metadata, not loose under <metadata>.

func TestSpecEditsLandInTheLegacyWrappers(t *testing.T) {
	path := buildEpub(t, opfWrappers)
	desc, series, index := "A new description.", "Wonderland", "3"
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Description: &desc, Series: &series, SeriesIndex: &index}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	if el := md.FindElement("dc-metadata/description"); el == nil || el.Text() != desc {
		t.Errorf("dc:description = %v, want %q inside dc-metadata per §2.2", el, desc)
	}
	// The series meta the file already had is updated where it sits; the index
	// is new, so it has to be created inside the wrapper rather than beside it.
	if el := md.FindElement("x-metadata/meta[@name='calibre:series']"); el == nil || el.SelectAttrValue("content", "") != series {
		t.Errorf("calibre:series = %v, want %q still inside x-metadata", el, series)
	}
	if el := md.FindElement("x-metadata/meta[@name='calibre:series_index']"); el == nil || el.SelectAttrValue("content", "") != index {
		t.Errorf("calibre:series_index = %v, want %q inside x-metadata", el, index)
	}
	for _, p := range []string{"description", "meta[@name='calibre:series']", "meta[@name='calibre:series_index']"} {
		if md.FindElement(p) != nil {
			t.Errorf("%s was written loose under <metadata>, outside the wrappers", p)
		}
	}

	// And the reader finds them where the writer put them.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != desc {
		t.Errorf("description = %q, want %q read back through dc-metadata", bib.Description, desc)
	}
	if bib.Series == nil || bib.Series.Name != series || bib.Series.Index != index {
		t.Errorf("series = %+v, want %q at %s read back through x-metadata", bib.Series, series, index)
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

// --- opf: attributes need a declared prefix ----------------------------------
//
// XML Namespaces §6.2 (Namespace Defaulting):
//   "The default namespace ... does not apply directly to attributes."
//
// So an EPUB 2 file may bind the OPF namespace as the default and still need
// xmlns:opf declared before opf:role and opf:file-as can be written. Writing
// them unprefixed would put them in no namespace, which is not what OPF 2.0
// §2.2.6 describes.

func TestSpecEPUB2AttributesGetADeclaredPrefix(t *testing.T) {
	// epub2() declares xmlns:opf; this file binds OPF as the default only.
	opf := strings.Replace(epub2(`    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <dc:language>en</dc:language>`), ` xmlns:opf="http://www.idpf.org/2007/opf"`, "", 1)

	path := buildEpub(t, opf)
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := epub.Rewrite(path, book(t, path), model.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	raw := string(readEntry(t, path, opfPath))
	if !strings.Contains(raw, `xmlns:opf="http://www.idpf.org/2007/opf"`) {
		t.Error("no xmlns:opf declaration was added for the prefixed attributes")
	}
	c := metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:role", ""); got != "aut" {
		t.Errorf("opf:role = %q, want aut", got)
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want the sort name", got)
	}

	// The reader resolves the attributes under the prefix the writer declared.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want one author sorting as Rand, Ann", bib.Authors)
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

// epub3 and epub2 wrap a <metadata> body in the smallest conforming package of
// each version, so a fixture shows only the metadata its test is about. The
// internal tests have the same helper as opf3With; it is not reachable from
// this package. Tests needing a different manifest keep a full literal.
func epub3(meta string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`
}

func epub2(meta string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="pub-id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
` + meta + `
  </metadata>
  <manifest>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`
}
