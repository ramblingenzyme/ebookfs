package epub_test

import (
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/epub"
)

var opfSeriesNoIndexV3 = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Lonely Book</dc:title>`,
	`<dc:creator id="creator1">Jane Doe</dc:creator>`,
	`<meta refines="#creator1" property="role">aut</meta>`,
	epubtest.Collection("c1", "Lonely Series", "series", ""),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB3()

var opfSeriesNoIndexV2 = epubtest.Pkg{Meta: epubtest.Metas(
	`<dc:title>Lonely Book</dc:title>`,
	`<dc:creator opf:role="aut">Jane Doe</dc:creator>`,
	epubtest.CalibreSeries("Lonely Series", ""),
), Manifest: epubtest.ChapterOnlyManifest}.EPUB2()

// Index 1 is calibre's default.
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
