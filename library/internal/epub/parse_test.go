package epub

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// --- parser-only fixtures & helpers ----------------------------------------

// multiRootContainer lists two package rootfiles where the first does not exist
// in the zip — the shape seen in some Kobo epubs.
const multiRootContainer = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/missing.opf" media-type="application/oebps-package+xml"/>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

// opfMarkupCoverImage mislabels an XHTML cover page with
// properties="cover-image"; the real raster cover is reached via <meta name="cover">.
var opfMarkupCoverImage = opf3Meta(`
    <dc:title>Original Title</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta name="cover" content="real-cover"/>`,
	`<item id="coverpage" href="coverpage.xhtml" media-type="application/xhtml+xml" properties="cover-image"/>
    <item id="real-cover" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`,
)

// opfSeriesSetCollection carries an EPUB 3 belongs-to-collection of type "set"
// (a publisher bundle, not a series) alongside a legacy calibre:series. The set
// must be ignored so the real series is the one read.
var opfSeriesSetCollection = opf3Meta(`
    <dc:title>Box Set Book</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta property="belongs-to-collection" id="c1">Some Box Set</meta>
    <meta refines="#c1" property="collection-type">set</meta>
    <meta name="calibre:series" content="Real Series"/>
    <meta name="calibre:series_index" content="3"/>`,
	"",
)

// opfSeriesNoIndexV3 is an EPUB 3 series collection with no group-position; the
// index should default to 1.
var opfSeriesNoIndexV3 = opf3Meta(`
    <dc:title>Lonely Book</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta property="belongs-to-collection" id="c1">Lonely Series</meta>
    <meta refines="#c1" property="collection-type">series</meta>`,
	"",
)

// opfSeriesNoIndexV2 is an EPUB 2 calibre:series with no calibre:series_index;
// the index should default to 1.
var opfSeriesNoIndexV2 = opf2Meta(`
    <dc:title>Lonely Book</dc:title>
    <dc:creator opf:role="aut">Jane Doe</dc:creator>
    <meta name="calibre:series" content="Lonely Series"/>`,
)

// opf3Meta wraps a metadata block and an optional <manifest> body in an
// EPUB 3 package skeleton. An empty manifest uses the default single-chapter
// entry so callers need only supply their <metadata> children.
func opf3Meta(metadata, manifest string) string {
	if manifest == "" {
		manifest = `<item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`
	}
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    ` + metadata + `
  </metadata>
  <manifest>
    ` + manifest + `
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`
}

// opf2Meta wraps a metadata block in an EPUB 2 package skeleton.
func opf2Meta(metadata string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    ` + metadata + `
  </metadata>
  <manifest>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="ch1"/></spine>
</package>`
}

// opfWithDates builds a minimal EPUB 2 package whose <metadata> carries the
// given raw <dc:date ...> elements, for exercising publication-date selection.
func opfWithDates(dateXML string) string {
	return opf2Meta(`
    <dc:title>Dated Book</dc:title>
    <dc:creator opf:role="aut">Jane Doe</dc:creator>
    ` + dateXML)
}

// opfV3WithModified is an EPUB 3 package with a publication dc:date and a
// dcterms:modified meta; the latter is not a dc:date and must not be read as the
// publication date.
var opfV3WithModified = opf3Meta(`
    <dc:title>V3 Book</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <dc:date>2015-06-01</dc:date>
    <meta property="dcterms:modified">2022-09-09T00:00:00Z</meta>`,
	"",
)

func withContainer(entries []entry, container string) []entry {
	out := make([]entry, len(entries))
	copy(out, entries)
	for i := range out {
		if out[i].name == "META-INF/container.xml" {
			out[i].data = []byte(container)
		}
	}
	return out
}

func withMimetype(entries []entry, value string) []entry {
	out := make([]entry, len(entries))
	copy(out, entries)
	for i := range out {
		if out[i].name == "mimetype" {
			out[i].data = []byte(value)
		}
	}
	return out
}

func withoutEntry(entries []entry, name string) []entry {
	var out []entry
	for _, e := range entries {
		if e.name != name {
			out = append(out, e)
		}
	}
	return out
}

// --- cover resolution ------------------------------------------------------

func TestTranslateCoverSkipsMarkupCoverImage(t *testing.T) {
	path := writeEpub(t, baseEntries(opfMarkupCoverImage))
	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverPath != "OEBPS/cover.jpg" {
		t.Errorf("cover path = %q, want OEBPS/cover.jpg (markup cover-image must be skipped)", book.CoverPath)
	}
}

// A percent-encoded cover href must resolve to the literal zip entry so the
// cover is found by both Parse and the WriteCover/Reader lookups.
func TestParseResolvesEncodedCoverHref(t *testing.T) {
	opfEncoded := opf3Meta(`
    <dc:title>Original Title</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>`,
		`<item id="cover-img" href="cover%20image.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`,
	)
	entries := []entry{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
		{name: "META-INF/container.xml", data: []byte(containerXML)},
		{name: "OEBPS/content.opf", data: []byte(opfEncoded)},
		{name: "OEBPS/cover image.jpg", data: coverBytes}, // literal space in the entry name
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	}
	path := writeEpub(t, entries)

	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.CoverPath != "OEBPS/cover image.jpg" {
		t.Fatalf("cover path = %q, want OEBPS/cover image.jpg", book.CoverPath)
	}
	r, err := OpenReader(path, book.CoverPath)
	if err != nil {
		t.Fatalf("OpenReader failed for an encoded-href cover: %v", err)
	}
	defer r.Close()
	got, err := r.Cover()
	if err != nil {
		t.Fatalf("Reader.Cover failed for an encoded-href cover: %v", err)
	}
	if !bytes.Equal(got, coverBytes) {
		t.Errorf("extracted cover bytes mismatch")
	}
}

// TestParseResolvesEncodedRootfilePath is the container-side half of the test
// above. §4.2.6.3.1.3 makes full-path "a path-relative-scheme-less-URL string",
// so a package document at "OEBPS/My Book.opf" is declared as "My%20Book.opf"
// and the zip entry it names holds the decoded form.
//
// Undecoded, the entry lookup misses and Parse reports ErrRootfileMissing — the
// book is not merely mis-read but unopenable, blamed on a rootfile that is
// present. The write path resolves the OPF through the same function, so an
// edit has to find it too.
func TestParseResolvesEncodedRootfilePath(t *testing.T) {
	const container = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/My%20Book.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

	path := writeEpub(t, []entry{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
		{name: "META-INF/container.xml", data: []byte(container)},
		{name: "OEBPS/My Book.opf", data: []byte(opf3)}, // literal space in the entry name
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	})

	if _, err := Parse(path); err != nil {
		t.Fatalf("Parse failed for a percent-encoded full-path: %v", err)
	}
	if _, err := writeBib(path, model.Edits{Title: new("Another Title")}); err != nil {
		t.Fatalf("edit failed for a percent-encoded full-path: %v", err)
	}
	bib, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Another Title" {
		t.Errorf("title = %q, want the edit to have landed in the resolved package document", bib.Title)
	}
}

// TestParseCollapsesRootfileMediaType covers the other container attribute read
// through encoding/xml, which does not apply XML 1.0 §3.3.3 attribute-value
// normalization. §4.2.6.3.1.3 requires the value to be the package media type;
// a container that wraps it would otherwise have every rootfile skipped and
// report ErrNoRootfile for a package document that is right there.
func TestParseCollapsesRootfileMediaType(t *testing.T) {
	const container = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf"
              media-type="
                application/oebps-package+xml
              "/>
  </rootfiles>
</container>`

	path := writeEpub(t, []entry{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
		{name: "META-INF/container.xml", data: []byte(container)},
		{name: "OEBPS/content.opf", data: []byte(opf3)},
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	})

	if _, err := Parse(path); err != nil {
		t.Fatalf("Parse failed for a padded rootfile media-type: %v", err)
	}
}

// TestParseAndWriteAgreeOnADuplicateEntry pins that a read and the edit that
// follows it resolve a duplicated entry name the same way. A zip may carry two
// entries under one name — badly repacked epubs do — and the two sides had
// opposite rules: Parse built its filemap by assignment, so the last copy won,
// while findEntry on the write side returns the first.
//
// Disagreeing means an edit is computed from one copy's metadata and reported
// from the other's, which is invisible until the two copies differ. Either rule
// would do; what matters is that it is one rule.
func TestParseAndWriteAgreeOnADuplicateEntry(t *testing.T) {
	first := strings.Replace(opf3, "Original Title", "First Copy", 1)
	second := strings.Replace(opf3, "Original Title", "Second Copy", 1)

	path := writeEpub(t, []entry{
		{name: "mimetype", data: []byte(mimetypeValue), store: true},
		{name: "META-INF/container.xml", data: []byte(containerXML)},
		{name: "OEBPS/content.opf", data: []byte(first)},
		{name: "OEBPS/content.opf", data: []byte(second)}, // same name, different content
		{name: "OEBPS/cover.jpg", data: coverBytes},
		{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
	})

	bib, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "First Copy" {
		t.Errorf("title = %q, want First Copy — Parse must resolve it the way findEntry does", bib.Title)
	}

	// The edit is computed from whichever copy the writer reads; the re-parse
	// has to see the result, which it only can if both picked the same one.
	if _, err := writeBib(path, model.Edits{Title: new("Edited Title")}); err != nil {
		t.Fatal(err)
	}
	bib, err = Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Edited Title" {
		t.Errorf("title = %q, want the edit to be visible to the next read", bib.Title)
	}
}

// TestParseRootfilePathEdgeCases covers what decoding full-path must not break.
// §4.2.6.3.1.3 makes it a path-relative-scheme-less-URL, so %20 decodes — but
// url.Parse would also read "C:/..." as a scheme and truncate at '#' or '?',
// silently naming an entry the archive does not hold. PathUnescape decodes the
// escapes and touches nothing else.
//
// The literal case is the other direction: a producer that wrote an unencoded
// name into both container.xml and the zip has an entry whose name really does
// contain '%20', so the raw value has to be tried when the decoded one misses.
func TestParseRootfilePathEdgeCases(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared string // full-path as written in container.xml
		entry    string // the zip entry that actually exists
	}{
		{"percent-encoded space", "OEBPS/My%20Book.opf", "OEBPS/My Book.opf"},
		{"literal percent-encoding in the entry name", "OEBPS/My%20Book.opf", "OEBPS/My%20Book.opf"},
		{"fragment character", "OEBPS/a#b.opf", "OEBPS/a#b.opf"},
		{"query character", "OEBPS/a?b.opf", "OEBPS/a?b.opf"},
		{"drive-letter shape", "C:/content.opf", "C:/content.opf"},
		{"stray percent", "OEBPS/100%.opf", "OEBPS/100%.opf"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			container := `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="` + tc.declared + `" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

			path := writeEpub(t, []entry{
				{name: "mimetype", data: []byte(mimetypeValue), store: true},
				{name: "META-INF/container.xml", data: []byte(container)},
				{name: tc.entry, data: []byte(opf3)},
				{name: "OEBPS/cover.jpg", data: coverBytes},
				{name: "OEBPS/chapter1.xhtml", data: chapterBytes},
			})

			if _, err := Parse(path); err != nil {
				t.Fatalf("full-path %q naming entry %q: %v", tc.declared, tc.entry, err)
			}
		})
	}
}

// --- container & mimetype validation ---------------------------------------

func TestParseRejectsNonZip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.epub")
	if err := os.WriteFile(p, []byte("this is plainly not a zip archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(p); !errors.Is(err, ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestParseReportsMissingFile(t *testing.T) {
	if _, err := Parse(filepath.Join(t.TempDir(), "absent.epub")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestParseRejectsWrongMimetype(t *testing.T) {
	p := writeEpub(t, withMimetype(baseEntries(opf3), "application/zip"))
	if _, err := Parse(p); !errors.Is(err, ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestParseRejectsMissingMimetype(t *testing.T) {
	p := writeEpub(t, withoutEntry(baseEntries(opf3), "mimetype"))
	if _, err := Parse(p); !errors.Is(err, ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestParseToleratesMimetypeWhitespace(t *testing.T) {
	// A trailing newline on the mimetype is tolerated (trimmed), matching calibre.
	p := writeEpub(t, withMimetype(baseEntries(opf3), "application/epub+zip\n"))
	if _, err := Parse(p); err != nil {
		t.Fatalf("Parse rejected a whitespace-padded mimetype: %v", err)
	}
}

// Kobo epubs sometimes declare several <rootfile> entries where only one
// exists; the absent ones must be skipped on both the read and write paths.
func TestMultipleRootfilesKobo(t *testing.T) {
	path := writeEpub(t, withContainer(baseEntries(opf3), multiRootContainer))

	book, err := Parse(path)
	if err != nil {
		t.Fatalf("Parse failed on Kobo multi-rootfile epub: %v", err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title", book.Title)
	}

	edited, err := writeBib(path, model.Edits{Title: new("Edited Title")})
	if err != nil {
		t.Fatalf("writeBib failed on Kobo multi-rootfile epub: %v", err)
	}
	if edited.Title != "Edited Title" {
		t.Errorf("edited title = %q, want Edited Title", edited.Title)
	}
}

// A belongs-to-collection of type "set" is not a series, so it must be ignored
// and the legacy calibre:series read instead — not mistaken for the series.
func TestTranslateSeriesSetCollectionIgnored(t *testing.T) {
	path := writeEpub(t, baseEntries(opfSeriesSetCollection))
	book, err := Parse(path)
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

// Publication-date selection across the opf:event vocabulary: an explicit
// "publication" wins; otherwise evented dates are dropped and only a lone
// untagged dc:date is used, with genuinely ambiguous cases left unset.
func TestTranslateDateSelection(t *testing.T) {
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
			// Selection is by authored count, so the unreadable sibling cannot
			// resolve the ambiguity by dropping out — it stays two untagged dates.
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
			path := writeEpub(t, baseEntries(opfWithDates(tc.dates)))
			book, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			if book.Pubdate != tc.want {
				t.Errorf("pubdate = %q, want %q", book.Pubdate, tc.want)
			}
		})
	}
}

// EPUB 3 stores last-modified as a meta property, not a dc:date, so it must not
// be mistaken for the publication date.
func TestTranslateDateIgnoresDctermsModified(t *testing.T) {
	path := writeEpub(t, baseEntries(opfV3WithModified))
	book, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if book.Pubdate != "2015-06-01" {
		t.Errorf("pubdate = %q, want 2015-06-01 (dcterms:modified must be ignored)", book.Pubdate)
	}
}

// A series with no position defaults to index 1 (calibre's convention), not the
// float64 zero value, which would render as "0. Title" in the by-series view.
func TestTranslateSeriesDefaultsIndexToOne(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  string
	}{
		{"epub3 collection without group-position", opfSeriesNoIndexV3},
		{"epub2 calibre:series without index", opfSeriesNoIndexV2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEpub(t, baseEntries(tc.opf))
			book, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			if book.Series == nil || book.Series.Name != "Lonely Series" {
				t.Fatalf("series = %v, want Lonely Series", book.Series)
			}
			if book.Series == nil || book.Series.Index != "1" {
				t.Errorf("series index = %v, want 1 (calibre default)", book.Series.Index)
			}
		})
	}
}
