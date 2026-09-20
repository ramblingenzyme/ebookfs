// The rules Parse applies that the epub package does not: a book must have a
// title and authors to be filed anywhere, and a series position the spec
// disallows must still display.

package epub_test

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

// opfSeriesNoIndexV3 is an EPUB 3 series collection with no group-position; the
// index should default to 1.
var opfSeriesNoIndexV3 = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Lonely Book</dc:title>`,
	`<dc:creator id="creator1">Jane Doe</dc:creator>`,
	`<meta refines="#creator1" property="role">aut</meta>`,
	epubtest.Collection("c1", "Lonely Series", "series", ""),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB3()

// opfSeriesNoIndexV2 is an EPUB 2 calibre:series with no calibre:series_index;
// the index should default to 1.
var opfSeriesNoIndexV2 = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Lonely Book</dc:title>`,
	`<dc:creator opf:role="aut">Jane Doe</dc:creator>`,
	epubtest.CalibreSeries("Lonely Series", ""),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB2()

// A series with no position defaults to index 1 (calibre's convention), not the
// float64 zero value, which would render as "0. Title" in the by-series view.
func TestParseDefaultsAMalformedSeriesIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  epubtest.PackageDoc
	}{
		{"epub3 collection without group-position", opfSeriesNoIndexV3},
		{"epub2 calibre:series without index", opfSeriesNoIndexV2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, epubtest.BaseEntries(tc.opf))
			book, err := epub.Parse(path)
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

// A book with no usable title or author has nowhere to live: the store builds
// every path from both. The epub package reports what the file says, so the
// refusal is this layer's.
func TestParseRefusesAnUnfilableBook(t *testing.T) {
	for _, tc := range []struct{ name, meta, want string }{
		{"no title", `    <dc:title></dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>`, "no title"},
		{"no authors", `    <dc:creator id="c1">   </dc:creator>`, "no authors"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := epub.Parse(epubtest.Build(t, epubtest.EPUB3(tc.meta)))
			if err == nil {
				t.Fatal("an unfilable book parsed anyway")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to say %q", err, tc.want)
			}
		})
	}
}
