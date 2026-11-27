package pdf_test

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/pdftest"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf"
)

const (
	calibreNS = `xmlns:calibre="http://calibre-ebook.com/xmp-namespace" xmlns:calibreSI="http://calibre-ebook.com/xmp-namespace-series-index"`
	idqNS     = `xmlns:xmpidq="http://ns.adobe.com/xmp/Identifier/qual/1.0/"`
	prismNS   = `xmlns:prism="http://prismstandard.org/namespaces/basic/2.0/"`
)

// calibre writes its series as a qualified value: the name in rdf:value, the
// position in a calibreSI:series_index qualifier.
func TestOpenReadsCalibreFields(t *testing.T) {
	for name, series := range map[string]string{
		"inline resource":    `<calibre:series ` + calibreNS + ` rdf:parseType="Resource"><rdf:value>Discworld</rdf:value><calibreSI:series_index>3.00</calibreSI:series_index></calibre:series>`,
		"nested description": `<calibre:series ` + calibreNS + `><rdf:Description calibreSI:series_index="3.00"><rdf:value>Discworld</rdf:value></rdf:Description></calibre:series>`,
	} {
		t.Run(name, func(t *testing.T) {
			doc := pdftest.Doc{XMP: pdftest.Packet(series + `
<calibre:title_sort ` + calibreNS + `>Colour of Magic, The</calibre:title_sort>
<calibre:author_sort ` + calibreNS + `>Pratchett, Terry &amp; Kidby, Paul</calibre:author_sort>`)}

			eachLayout(t, doc, func(t *testing.T, path string) {
				b := open(t, path)
				if !reflect.DeepEqual(b.Series, &pdf.Series{Name: "Discworld", Index: "3.00"}) {
					t.Errorf("Series = %+v", b.Series)
				}

				if b.SortTitle != "Colour of Magic, The" || b.AuthorSort != "Pratchett, Terry & Kidby, Paul" {
					t.Errorf("SortTitle = %q, AuthorSort = %q", b.SortTitle, b.AuthorSort)
				}
			})
		})
	}
}

// The series is written as calibre reads it, with the name in rdf:value even
// when there is no position to qualify it with.
func TestSaveWritesTheSeriesAsCalibreDoes(t *testing.T) {
	for _, tc := range []struct {
		series pdf.Series
		want   string
	}{
		{pdf.Series{Name: "Saga", Index: "2"}, `<calibre:series rdf:parseType="Resource"><rdf:value>Saga</rdf:value><calibreSI:series_index>2</calibreSI:series_index></calibre:series>`},
		{pdf.Series{Name: "Saga"}, `<calibre:series rdf:parseType="Resource"><rdf:value>Saga</rdf:value></calibre:series>`},
	} {
		path := pdftest.Build(t, pdftest.Doc{})
		mustSave(t, path, func(b *pdf.Book) { b.Series = &tc.series })

		if !bytes.Contains(read(t, path), []byte(tc.want)) {
			t.Errorf("Series %+v: the packet does not hold %s", tc.series, tc.want)
		}

		if got := open(t, path).Series; !reflect.DeepEqual(got, &tc.series) {
			t.Errorf("Series %+v reads back as %+v", tc.series, got)
		}
	}
}

// Identifiers are keyed as pkg/epub keys them (DECISIONS #24): by stated
// scheme, lowercased, else by the value's urn: namespace, else unknown,
// unknown-2. The first value for a scheme wins.
func TestIdentifiers(t *testing.T) {
	doc := pdftest.Doc{XMP: pdftest.Packet(`
<xmp:Identifier ` + idqNS + `><rdf:Bag>
  <rdf:li rdf:parseType="Resource"><xmpidq:Scheme>ISBN</xmpidq:Scheme><rdf:value>urn:isbn:9780552124751</rdf:value></rdf:li>
  <rdf:li><rdf:Description xmpidq:Scheme="calibre"><rdf:value>1234</rdf:value></rdf:Description></rdf:li>
  <rdf:li>urn:uuid:0d3b7c1e-1111-2222-3333-444455556666</rdf:li>
  <rdf:li>first unschemed</rdf:li>
</rdf:Bag></xmp:Identifier>
<dc:identifier>second unschemed</dc:identifier>
<prism:doi ` + prismNS + `>10.1000/xyz</prism:doi>
<prism:isbn ` + prismNS + `>0000000000</prism:isbn>`)}

	want := map[string]string{
		"isbn":      "9780552124751",
		"calibre":   "1234",
		"uuid":      "0d3b7c1e-1111-2222-3333-444455556666",
		"unknown":   "first unschemed",
		"unknown-2": "second unschemed",
		"doi":       "10.1000/xyz",
	}

	eachLayout(t, doc, func(t *testing.T, path string) {
		if got := open(t, path).Identifiers(); !reflect.DeepEqual(got, want) {
			t.Errorf("Identifiers =\n %v\nwant\n %v", got, want)
		}
	})

	if got := open(t, pdftest.Build(t, pdftest.Doc{})).Identifiers(); len(got) != 0 {
		t.Errorf("without a packet, Identifiers = %v", got)
	}
}

func TestPubdate(t *testing.T) {
	for name, tc := range map[string]struct{ props, want string }{
		"dc:date, as calibre writes it": {`<dc:date><rdf:Seq><rdf:li>2001-05-01T00:00:00+00:00</rdf:li><rdf:li>2003</rdf:li></rdf:Seq></dc:date>`, "2001-05-01T00:00:00+00:00"},
		"PRISM, as publishers write it": {`<prism:publicationDate ` + prismNS + `>2019-03</prism:publicationDate>`, "2019-03"},
		"dc:date wins over PRISM":       {`<dc:date><rdf:Seq><rdf:li>2001</rdf:li></rdf:Seq></dc:date><prism:publicationDate ` + prismNS + `>2019</prism:publicationDate>`, "2001"},
		"neither":                       {`<xmp:CreateDate>2020-01-01</xmp:CreateDate>`, ""},
	} {
		t.Run(name, func(t *testing.T) {
			if got := open(t, pdftest.Build(t, pdftest.Doc{XMP: pdftest.Packet(tc.props)})).Pubdate(); got != tc.want {
				t.Errorf("Pubdate = %q, want %q", got, tc.want)
			}
		})
	}
}
