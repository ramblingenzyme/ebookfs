package epub_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

func TestOpenResolvesEncodedCoverHref(t *testing.T) {
	opfEncoded := epubtest.Pkg{Meta: `    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>`, Manifest: `<item id="cover-img" href="cover%20image.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()
	entries := []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(string(opfEncoded))},
		{Name: "OEBPS/cover image.jpg", Data: epubtest.CoverBytes}, // literal space in the entry name
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	}
	path := epubtest.WriteEpub(t, entries)

	book, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverPath() != "OEBPS/cover image.jpg" {
		t.Fatalf("cover path = %q, want OEBPS/cover image.jpg", book.CoverPath())
	}
	got, err := open(t, path).Cover()
	if err != nil {
		t.Fatalf("Reader.Cover failed for an encoded-href cover: %v", err)
	}
	if !bytes.Equal(got, epubtest.CoverBytes) {
		t.Errorf("extracted cover bytes mismatch")
	}
}

func TestFirstDescriptionWins(t *testing.T) {

	var opf = epubtest.EPUB3(`    <dc:description>FIRST description.</dc:description>
    <dc:description>SECOND description.</dc:description>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != "FIRST description." {
		t.Errorf("description = %q, want the first, consistent with every other repeatable field", bib.Description)
	}
}

// The spec says nothing here. calibre keeps looking, and so does this.
func TestDanglingCoverMetaFallsThrough(t *testing.T) {

	opf := epubtest.EPUB2(`    <meta name="cover" content="does-not-exist"/>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath() == "" {
		t.Error("no cover found; a stale cover meta should not suppress the fallback")
	}
}

// Pubdate needs exactly one untagged date, so counting the empty one would
// make two and lose the date.
func TestEmptyDateIsSkipped(t *testing.T) {
	bib, err := parse(t, epubtest.Build(t, epubtest.EPUB3(`    <dc:date>   </dc:date>
    <dc:date>2020-01-02</dc:date>`)))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Pubdate() != "2020-01-02" {
		t.Errorf("pubdate = %q, want the one date that carries a value", bib.Pubdate())
	}
}

// Rejecting the book would lose it over an element that carries nothing.
func TestEmptyCreatorIsSkippedNotFatal(t *testing.T) {
	t.Run("skipped alongside a usable one", func(t *testing.T) {
		bib, err := parse(t, epubtest.Build(t, epubtest.EPUB3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:creator id="c2">   </dc:creator>`)))
		if err != nil {
			t.Fatalf("a usable author sits beside the empty creator: %v", err)
		}
		if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand"}) {
			t.Errorf("authors = %v, want [Ann Rand]", got)
		}
	})

}

func TestEmptyFirstTitleFallsThrough(t *testing.T) {

	var opf = epubtest.EPUB3(`    <dc:title>   </dc:title>
    <dc:title>The Real Title</dc:title>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatalf("a usable title follows the empty one: %v", err)
	}
	if bib.Title != "The Real Title" {
		t.Errorf("title = %q", bib.Title)
	}
}

// With no refines, the meta refines the whole package (§5.3.6), not the
// creator beside it.
func TestUnrefinedMetaIsNotACreatorsSortName(t *testing.T) {
	opf := epubtest.EPUB3(`    <meta property="file-as">Someone, Else</meta>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "" {
		t.Errorf("authors = %+v, want no sort name", bib.Authors)
	}
}

// Files saved by an earlier version of the contributor writer repeat a role on
// one element. Reporting each copy would give the index two identical rows.
func TestRepeatedContributorRoleReadsOnce(t *testing.T) {
	opf := epubtest.EPUB3(`    <dc:contributor id="c1">Jane Doe</dc:contributor>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">trl</meta>
    <meta refines="#c1" property="role" scheme="marc:relators">edt</meta>`)
	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	want := []epub.Contributor{{Name: "Jane Doe", Role: "edt"}, {Name: "Jane Doe", Role: "trl"}}
	if !slices.Equal(bib.Contributors, want) {
		t.Errorf("contributors = %v, want %v", bib.Contributors, want)
	}
}

// Neither spec describes this fallback. calibre takes the first match; this
// takes the last.
func TestCoverHeuristicTakesTheLastMatch(t *testing.T) {
	opf := epubtest.Pkg{Manifest: `<item id="cover-thumb" href="thumb.jpg" media-type="image/jpeg"/>
    <item id="cover.jpg" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath() != "OEBPS/cover.jpg" {
		t.Errorf("cover = %q, want the last matching item (calibre would take thumb.jpg)", bib.CoverPath())
	}
}

func TestIdentifierKeying(t *testing.T) {
	tests := []struct {
		name string
		opf  epubtest.PackageDoc
		want map[string]string
	}{{
		name: "epub2 opf:scheme attribute",
		opf:  epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="ISBN">9780123456789</dc:identifier>`),
		want: map[string]string{"isbn": "9780123456789"},
	}, {
		// etree matches attributes by local name, so a bare scheme reads like
		// opf:scheme.
		name: "unprefixed scheme attribute",
		opf:  epubtest.EPUB2(`    <dc:identifier id="BookId" scheme="ASIN">B00X57B4KG</dc:identifier>`),
		want: map[string]string{"asin": "B00X57B4KG"},
	}, {
		name: "onix codelist 5 identifier-type",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id">9780123456789</dc:identifier>
    <meta refines="#pub-id" property="identifier-type" scheme="onix:codelist5">15</meta>`),
		want: map[string]string{"isbn": "9780123456789"},
	}, {
		name: "unschemed identifier-type is a name, not a code",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id">10.1234/beta</dc:identifier>
    <meta refines="#pub-id" property="identifier-type">DOI</meta>`),
		want: map[string]string{"doi": "10.1234/beta"},
	}, {
		// A code from an unknown list is ignored. With no URN in the value, the
		// id is used.
		name: "identifier-type from another code list falls through",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id">12345</dc:identifier>
    <meta refines="#pub-id" property="identifier-type" scheme="marc:relators">15</meta>`),
		want: map[string]string{"pub-id": "12345"},
	}, {
		// 22 is ONIX's "URN", which says the type is in the value.
		name: "unrecognised onix code falls through to the urn",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id">urn:isbn:9780123456789</dc:identifier>
    <meta refines="#pub-id" property="identifier-type" scheme="onix:codelist5">22</meta>`),
		want: map[string]string{"isbn": "9780123456789"},
	}, {
		// The EPUB 2 form of the case above.
		name: "opf:scheme of urn falls through to the value",
		opf:  epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="URN">urn:isbn:9780123456789</dc:identifier>`),
		want: map[string]string{"isbn": "9780123456789"},
	}, {
		name: "urn in the value names the scheme",
		opf:  epubtest.EPUB3(`    <dc:identifier id="pub-id">urn:uuid:A1B0D67E</dc:identifier>`),
		want: map[string]string{"uuid": "A1B0D67E"},
	}, {
		name: "urn: and the NID are case-insensitive, the value is not",
		opf:  epubtest.EPUB3(`    <dc:identifier id="pub-id">URN:UUID:A1B0D67E</dc:identifier>`),
		want: map[string]string{"uuid": "A1B0D67E"},
	}, {
		// The urn prefix is dropped only when it repeats the scheme.
		name: "urn under an unrelated scheme is kept whole",
		opf:  epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="calibre">urn:uuid:A1B0D67E</dc:identifier>`),
		want: map[string]string{"calibre": "urn:uuid:A1B0D67E"},
	}, {
		name: "nothing names it, so the xml id does",
		opf:  epubtest.EPUB3(`    <dc:identifier id="BookId">12345</dc:identifier>`),
		want: map[string]string{"bookid": "12345"},
	}, {
		// Only the unique-identifier needs an id, so this is legal and common in
		// EPUB 2.
		name: "an identifier with no id at all is still carried",
		opf: epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="ISBN">9780123456789</dc:identifier>
    <dc:identifier>B00X57B4KG</dc:identifier>`),
		want: map[string]string{"isbn": "9780123456789", "unknown": "B00X57B4KG"},
	}, {
		name: "two unnamed identifiers are numbered, not dropped",
		opf: epubtest.EPUB2(`    <dc:identifier>B00X57B4KG</dc:identifier>
    <dc:identifier>12345</dc:identifier>`),
		want: map[string]string{"unknown": "B00X57B4KG", "unknown-2": "12345"},
	}, {
		// ISBN-10 and ISBN-13 share one scheme, and a map holds one value.
		name: "two identifiers on one scheme, first in document order wins",
		opf: epubtest.EPUB3(`    <dc:identifier id="isbn13">9780123456789</dc:identifier>
    <meta refines="#isbn13" property="identifier-type" scheme="onix:codelist5">15</meta>
    <dc:identifier id="isbn10">0123456789</dc:identifier>
    <meta refines="#isbn10" property="identifier-type" scheme="onix:codelist5">02</meta>`),
		want: map[string]string{"isbn": "9780123456789"},
	}, {
		name: "an empty identifier is not one",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id"></dc:identifier>
    <dc:identifier id="other">urn:uuid:1234</dc:identifier>`),
		want: map[string]string{"uuid": "1234"},
	}, {
		name: "several identifiers, each keyed on its own terms",
		opf: epubtest.EPUB3(`    <dc:identifier id="pub-id">urn:uuid:A1B0D67E</dc:identifier>
    <dc:identifier id="isbn">urn:isbn:9780123456789</dc:identifier>
    <meta refines="#isbn" property="identifier-type" scheme="onix:codelist5">15</meta>
    <dc:identifier id="mobi-asin">B00X57B4KG</dc:identifier>`),
		want: map[string]string{
			"uuid":      "A1B0D67E",
			"isbn":      "9780123456789",
			"mobi-asin": "B00X57B4KG",
		},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bib, err := parse(t, epubtest.Build(t, tt.opf))
			if err != nil {
				t.Fatal(err)
			}
			if !maps.Equal(bib.Identifiers(), tt.want) {
				t.Errorf("identifiers = %v, want %v", bib.Identifiers(), tt.want)
			}
		})
	}
}

// D.1.4 lets a document bind its own prefix to ONIX's code list, so the
// scheme is resolved rather than compared to the literal "onix:".
func TestIdentifierTypeInReboundVocabulary(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <dc:identifier id="pub-id">9780123456789</dc:identifier>
    <meta refines="#pub-id" property="identifier-type" scheme="onx:codelist5">15</meta>`, Attrs: `prefix="onx: http://www.editeur.org/ONIX/book/codelists/current.html#"`}.EPUB3()

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"isbn": "9780123456789"}
	if !maps.Equal(bib.Identifiers(), want) {
		t.Errorf("identifiers = %v, want %v", bib.Identifiers(), want)
	}
}

// calibre records an EPUB 2 sort title only in calibre:title_sort.
func TestOpenReadsCalibreTitleSortFromEPUB2(t *testing.T) {
	opf := epubtest.OPF2.With(`    <meta name="calibre:title_sort" content="Hobbit, The"/>`)
	bib, err := parse(t, epubtest.WriteEpub(t, epubtest.BaseEntries(opf)))
	if err != nil {
		t.Fatal(err)
	}
	if bib.SortTitle != "Hobbit, The" {
		t.Errorf("sort title = %q, want %q from calibre:title_sort", bib.SortTitle, "Hobbit, The")
	}
}

func TestSetCollectionIsNotASeries(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfSeriesSetCollection))
	book, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Series == nil || book.Series.Name != "Real Series" || book.Series.Index != "3" {
		t.Errorf("series = %+v, want Real Series at 3 (set collection should be ignored)", book.Series)
	}
}

func TestCoverSkipsAMarkupCoverImage(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfMarkupCoverImage))
	book, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverPath() != "OEBPS/cover.jpg" {
		t.Errorf("cover path = %q, want OEBPS/cover.jpg (markup cover-image must be skipped)", book.CoverPath())
	}
}

// The XHTML case above matches both halves of the media type check, so only
// this one fails if the "xml" half goes.
func TestCoverSkipsAnSVGCoverImage(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <meta name="cover" content="real-cover"/>`, Manifest: `<item id="svg-cover" href="cover.svg" media-type="image/svg+xml" properties="cover-image"/>
    <item id="real-cover" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()

	book, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverPath() != "OEBPS/cover.jpg" {
		t.Errorf("cover path = %q, want OEBPS/cover.jpg (svg cover-image must be skipped)", book.CoverPath())
	}
}

func TestPubdateSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dates string
		want  string
	}{
		{
			"publication wins over creation and modification",
			`<dc:date opf:event="publication">2016-11-29</dc:date>
     <dc:date opf:event="creation">2020-07-20</dc:date>
     <dc:date opf:event="modification">2019-05-12</dc:date>`,
			"2016-11-29",
		},
		{
			"untagged used when only a modification is tagged",
			`<dc:date opf:event="modification">2021-03-01</dc:date>
     <dc:date>2019-05-01</dc:date>`,
			"2019-05-01",
		},
		{
			"single untagged date",
			`<dc:date>2018-04-04</dc:date>`,
			"2018-04-04",
		},
		{
			"two untagged dates are ambiguous, left unset",
			`<dc:date>2019-01-01</dc:date>
     <dc:date>2021-01-01</dc:date>`,
			"",
		},
		{
			"only a creation date is not a publication date, left unset",
			`<dc:date opf:event="creation">1851-01-01</dc:date>`,
			"",
		},
		{
			// Dates are counted as written, not as parsed.
			"two untagged, one unreadable, still ambiguous",
			`<dc:date>2019-05-01</dc:date>
     <dc:date>not-a-date</dc:date>`,
			"",
		},
		{
			"publication date stored verbatim even if not ISO",
			`<dc:date opf:event="publication">not-a-date</dc:date>
     <dc:date>2019-05-01</dc:date>`,
			"not-a-date",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfWithDates(tc.dates)))
			book, err := parse(t, path)
			if err != nil {
				t.Fatal(err)
			}
			if book.Pubdate() != tc.want {
				t.Errorf("pubdate = %q, want %q", book.Pubdate(), tc.want)
			}
		})
	}
}

func TestPubdateIgnoresDctermsModified(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfV3WithModified))
	book, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Pubdate() != "2015-06-01" {
		t.Errorf("pubdate = %q, want 2015-06-01 (dcterms:modified must be ignored)", book.Pubdate())
	}
}

// The XHTML cover page is mislabelled cover-image. The real cover is reached
// through <meta name="cover">.
var opfMarkupCoverImage = epubtest.Pkg{Meta: `    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta name="cover" content="real-cover"/>`, Manifest: `<item id="coverpage" href="coverpage.xhtml" media-type="application/xhtml+xml" properties="cover-image"/>
    <item id="real-cover" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()

var opfSeriesSetCollection = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Box Set Book</dc:title>`,
	`<dc:creator id="creator1">Jane Doe</dc:creator>`,
	`<meta refines="#creator1" property="role">aut</meta>`,
	epubtest.Collection("c1", "Some Box Set", "set", ""),
	epubtest.CalibreSeries("Real Series", "3"),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB3()

func opfWithDates(dateXML string) epubtest.PackageDoc {
	return epubtest.Pkg{Meta: `    <dc:title>Dated Book</dc:title>
    <dc:creator opf:role="aut">Jane Doe</dc:creator>
    ` + dateXML, Manifest: epubtest.ChapterOnlyManifest}.EPUB2()
}

var opfV3WithModified = epubtest.Pkg{Meta: `    <dc:title>V3 Book</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <dc:date>2015-06-01</dc:date>
    <meta property="dcterms:modified">2022-09-09T00:00:00Z</meta>`, Manifest: epubtest.ChapterOnlyManifest}.EPUB3()
