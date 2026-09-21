// What Open reads out of a package document, and the rules it applies doing
// so: which of several elements is the one meant, what an empty or absent one
// means, and how a value the file states two ways is reconciled.
//
// Nothing here edits. A value is reported as the file carries it, so a book
// with no title reads back an empty Title rather than an error.

package epub_test

import (
	"bytes"
	"maps"
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
)

// A percent-encoded cover href must resolve to the literal zip entry so the
// cover is found by both the metadata read and the entry lookups.
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

// A legacy cover meta naming an id not in the manifest suppresses the
// id-heuristic fallback, so a broken pointer does worse than a missing one.
// The spec says nothing; calibre keeps looking.
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

// An empty dc:date is invalid, and skipping it matters more than for a title or
// creator: pubdate returns a date only when exactly one untagged dc:date has a
// value, so counting an empty one makes it two and the book loses its date.
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

// A stray empty creator costs nothing, and a file with no readable author at all
// still fails loudly. Rejecting the first would lose a whole book over an
// element carrying no information.
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

// --- an edit has to land somewhere ---

// A file-as meta with no refines, beside a creator with no id. Nothing links
// them, so the meta refines the package as a whole (§5.3.6) and must not be read
// as that creator's sort name.
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

// --- cover fallback ordering ---

// No cover-image property, no <meta name="cover">, just manifest ids containing
// "cover". Neither spec describes this. calibre takes the first such item and
// this takes the last, pinned rather than left to be discovered by a book
// showing the wrong cover.
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

// --- duplicate refinements ---

// The scheme keys a dc:identifier because that is what the value is. The XML id
// is a document-local handle and only the last resort (docs/DECISIONS.md #24).
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
		// etree matches an attribute by local name whatever prefix it carries, so a
		// bare spelling reads the same as opf:scheme. The write side documents
		// that in OPFAttr.Set.
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
		// A code list we do not know is not ours to read, so the value's own URN gets
		// the next turn. Here there is none, and the id is left.
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
		// The v2 spelling of the same thing: a scheme of urn says only that the
		// kind is in the value, so the value's own namespace answers and the
		// identifier is not keyed under "urn" with the prefix still attached.
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
		// The prefix is only redundant when it repeats the key. Under calibre's
		// own scheme it is part of what the value says.
		name: "urn under an unrelated scheme is kept whole",
		opf:  epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="calibre">urn:uuid:A1B0D67E</dc:identifier>`),
		want: map[string]string{"calibre": "urn:uuid:A1B0D67E"},
	}, {
		name: "nothing names it, so the xml id does",
		opf:  epubtest.EPUB3(`    <dc:identifier id="BookId">12345</dc:identifier>`),
		want: map[string]string{"bookid": "12345"},
	}, {
		// Only the unique-identifier target has to carry an id, so a second
		// dc:identifier without one is legal and common in v2 output. Nothing
		// names this one and there is no id to borrow, but the value is an
		// identifier all the same.
		name: "an identifier with no id at all is still carried",
		opf: epubtest.EPUB2(`    <dc:identifier id="BookId" opf:scheme="ISBN">9780123456789</dc:identifier>
    <dc:identifier>B00X57B4KG</dc:identifier>`),
		want: map[string]string{"isbn": "9780123456789", "unknown": "B00X57B4KG"},
	}, {
		// Numbered rather than sharing one key, so first-wins does not eat the
		// second: position is all that distinguishes them.
		name: "two unnamed identifiers are numbered, not dropped",
		opf: epubtest.EPUB2(`    <dc:identifier>B00X57B4KG</dc:identifier>
    <dc:identifier>12345</dc:identifier>`),
		want: map[string]string{"unknown": "B00X57B4KG", "unknown-2": "12345"},
	}, {
		// ISBN-10 and ISBN-13 are one scheme to us, and only one row can exist.
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

// D.1.4 lets a document bind its own prefix to ONIX's code list, so the scheme
// is matched through the vocabulary. A reader comparing the literal "onix:"
// would read this identifier as untyped.
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

// --- xml:lang and dir ---

// calibre records a v2 sort title in calibre:title_sort and nowhere else, so
// without this fallback every calibre-managed v2 book reads back with none.
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

// A belongs-to-collection of type "set" is not a series, so it must be ignored
// and the legacy calibre:series read instead. Not mistaken for the series.
func TestSetCollectionIsNotASeries(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(opfSeriesSetCollection))
	book, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Series == nil || book.Series.Name != "Real Series" {
		t.Errorf("series = %v, want Real Series (set collection should be ignored)", book.Series)
	}
	if book.Series == nil || book.Series.Index != "3" {
		t.Errorf("series index = %v, want 3", book.Series.Index)
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

// Publication-date selection across the opf:event vocabulary: an explicit
// "publication" wins; otherwise evented dates are dropped and only a lone
// untagged dc:date is used, with genuinely ambiguous cases left unset.
func TestPubdateSelection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		dates string
		want  string // "" means no date (zero time)
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
			// Selection is by authored count, so the unreadable sibling cannot resolve
			// the ambiguity by dropping out. It stays two untagged dates.
			"two untagged, one unreadable, still ambiguous",
			`<dc:date>2019-05-01</dc:date>
     <dc:date>not-a-date</dc:date>`,
			"",
		},
		{
			// A designated publication date is authoritative and stored verbatim,
			// even if it would not have been parseable as an ISO date.
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

// EPUB 3 stores last-modified as a meta property, not a dc:date, so it must not
// be mistaken for the publication date.
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

// opfMarkupCoverImage mislabels an XHTML cover page with
// properties="cover-image"; the real raster cover is reached via <meta name="cover">.
var opfMarkupCoverImage = epubtest.Pkg{Meta: `    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta name="cover" content="real-cover"/>`, Manifest: `<item id="coverpage" href="coverpage.xhtml" media-type="application/xhtml+xml" properties="cover-image"/>
    <item id="real-cover" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()

// opfSeriesSetCollection carries an EPUB 3 belongs-to-collection of type "set"
// (a publisher bundle, not a series) alongside a legacy calibre:series. The set
// must be ignored so the real series is the one read.
var opfSeriesSetCollection = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Box Set Book</dc:title>`,
	`<dc:creator id="creator1">Jane Doe</dc:creator>`,
	`<meta refines="#creator1" property="role">aut</meta>`,
	epubtest.Collection("c1", "Some Box Set", "set", ""),
	epubtest.CalibreSeries("Real Series", "3"),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB3()

// opfWithDates builds a minimal EPUB 2 package whose <metadata> carries the
// given raw <dc:date ...> elements, for exercising publication-date selection.
func opfWithDates(dateXML string) epubtest.PackageDoc {
	return epubtest.Pkg{Meta: `    <dc:title>Dated Book</dc:title>
    <dc:creator opf:role="aut">Jane Doe</dc:creator>
    ` + dateXML, Manifest: epubtest.ChapterOnlyManifest}.EPUB2()
}

// opfV3WithModified is an EPUB 3 package with a publication dc:date and a
// dcterms:modified meta; the latter is not a dc:date and must not be read as the
// publication date.
var opfV3WithModified = epubtest.Pkg{Meta: `    <dc:title>V3 Book</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <dc:date>2015-06-01</dc:date>
    <meta property="dcterms:modified">2022-09-09T00:00:00Z</meta>`, Manifest: epubtest.ChapterOnlyManifest}.EPUB3()
