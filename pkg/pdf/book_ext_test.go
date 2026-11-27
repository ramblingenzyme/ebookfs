package pdf_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/pdftest"
	"github.com/ramblingenzyme/ebookfs/pkg/pdf"
)

func TestOpenReadsTheInformationDictionary(t *testing.T) {
	doc := pdftest.Doc{Info: map[string]string{
		"Title":    "(A Title)",
		"Author":   "(Ann One, Bo Two)",
		"Subject":  "(What it is about)",
		"Keywords": "(alpha; beta)",
	}, Catalog: "/Lang (en-GB)"}

	eachLayout(t, doc, func(t *testing.T, path string) {
		want := pdf.Metadata{
			Title:       "A Title",
			Authors:     []string{"Ann One, Bo Two"},
			Description: "What it is about",
			Keywords:    "alpha; beta",
			Language:    "en-GB",
		}

		if got := open(t, path).Metadata; !equal(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}

func TestOpenReadsXMP(t *testing.T) {
	doc := pdftest.Doc{XMP: pdftest.Packet(`
<dc:title><rdf:Alt><rdf:li xml:lang="de">Titel</rdf:li><rdf:li xml:lang="x-default">Title</rdf:li></rdf:Alt></dc:title>
<dc:creator><rdf:Seq><rdf:li>Ann One</rdf:li><rdf:li>Bo Two</rdf:li></rdf:Seq></dc:creator>
<dc:description><rdf:Alt><rdf:li xml:lang="x-default">About</rdf:li></rdf:Alt></dc:description>
<dc:subject><rdf:Bag><rdf:li>one</rdf:li><rdf:li>two</rdf:li></rdf:Bag></dc:subject>
<dc:language><rdf:Bag><rdf:li>fr</rdf:li><rdf:li>en</rdf:li></rdf:Bag></dc:language>
<dc:publisher><rdf:Bag><rdf:li>Pub</rdf:li></rdf:Bag></dc:publisher>
<dc:rights><rdf:Alt><rdf:li xml:lang="x-default">CC-BY</rdf:li></rdf:Alt></dc:rights>
<dc:contributor><rdf:Bag><rdf:li>Ed Itor</rdf:li></rdf:Bag></dc:contributor>
<pdf:Keywords>k1, k2</pdf:Keywords>`)}

	eachLayout(t, doc, func(t *testing.T, path string) {
		want := pdf.Metadata{
			Title:        "Title",
			Authors:      []string{"Ann One", "Bo Two"},
			Description:  "About",
			Subjects:     []string{"one", "two"},
			Keywords:     "k1, k2",
			Language:     "fr",
			Publisher:    "Pub",
			Rights:       "CC-BY",
			Contributors: []string{"Ed Itor"},
		}

		if got := open(t, path).Metadata; !equal(got, want) {
			t.Errorf("got %+v\nwant %+v", got, want)
		}
	})
}

// XMP Part 1 §7.5 lets a simple property be an attribute of rdf:Description,
// and §7.8 lets a value carry qualifiers through rdf:value.
func TestOpenReadsXMPAttributeAndQualifiedForms(t *testing.T) {
	doc := pdftest.Doc{XMP: strings.Replace(pdftest.Packet(`
<dc:creator><rdf:Seq><rdf:li rdf:parseType="Resource"><rdf:value>Ann One</rdf:value><x:role xmlns:x="urn:x">aut</x:role></rdf:li></rdf:Seq></dc:creator>`),
		`rdf:about=""`, `rdf:about="" pdf:Keywords="from an attribute"`, 1)}

	b := open(t, pdftest.Build(t, doc))
	if b.Keywords != "from an attribute" || !reflect.DeepEqual(b.Authors, []string{"Ann One"}) {
		t.Errorf("Keywords = %q, Authors = %q", b.Keywords, b.Authors)
	}
}

func TestOpenPrefersXMPOverTheInformationDictionary(t *testing.T) {
	doc := pdftest.Doc{
		Info: map[string]string{"Title": "(Info Title)", "Author": "(Info Author)"},
		XMP:  pdftest.Packet(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">XMP Title</rdf:li></rdf:Alt></dc:title>`),
	}

	b := open(t, pdftest.Build(t, doc))
	if b.Title != "XMP Title" {
		t.Errorf("Title = %q, want the XMP value", b.Title)
	}

	if !reflect.DeepEqual(b.Authors, []string{"Info Author"}) {
		t.Errorf("Authors = %q, want the dictionary's, which XMP lacks", b.Authors)
	}
}

// A tool that knows only the information dictionary leaves the XMP stale, and
// says so with a ModDate later than the packet's MetadataDate.
func TestOpenPrefersANewerInformationDictionary(t *testing.T) {
	for _, tc := range []struct {
		name, modDate, want string
	}{
		{"newer", "(D:20240102030405Z)", "Info Title"},
		{"older", "(D:20200102030405Z)", "XMP Title"},
		{"PDF 1.7 trailing apostrophe", "(D:20240102030405+01'00')", "Info Title"},
		{"unparseable", "(yesterday)", "XMP Title"},
		{"month 13 rolls over into the next year", "(D:20221301)", "Info Title"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := pdftest.Doc{
				Info: map[string]string{"Title": "(Info Title)", "ModDate": tc.modDate},
				XMP: pdftest.Packet(`<dc:title><rdf:Alt><rdf:li xml:lang="x-default">XMP Title</rdf:li></rdf:Alt></dc:title>
<xmp:MetadataDate>2022-01-01T00:00:00Z</xmp:MetadataDate>`),
			}

			if got := open(t, pdftest.Build(t, doc)).Title; got != tc.want {
				t.Errorf("Title = %q, want %q", got, tc.want)
			}
		})
	}
}

// §7.9.2.2: a text string is UTF-16BE or UTF-8 after a byte order mark, and
// PDFDocEncoding otherwise.
func TestOpenDecodesTextStrings(t *testing.T) {
	for _, tc := range []struct {
		name, raw, want string
	}{
		{"literal escapes", `(a\(b\)\\c\nd\101\
e)`, "a(b)\\c\ndAe"},
		{"UTF-16BE", "<FEFF0054006900740065006C00200C9F>", "Titel ಟ"},
		{"UTF-16BE surrogate pair", "<FEFFD83DDE00>", "😀"},
		{"UTF-8", "<EFBBBF54C3AF>", "Tï"},
		{"PDFDocEncoding", `(\223ne \204 \240)`, "ﬁne — €"},
		{"Latin-1 range", "<E9>", "é"},
		{"odd hex digit", "<41 4>", "A@"},
		{"language escape", "<FEFF001B656E001B0048>", "H"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc := pdftest.Doc{Info: map[string]string{"Title": tc.raw}}
			if got := open(t, pdftest.Build(t, doc)).Title; got != tc.want {
				t.Errorf("Title = %q, want %q", got, tc.want)
			}
		})
	}
}

// §14.3.3 requires a text string; anything else is not a title.
func TestOpenIgnoresANonStringEntry(t *testing.T) {
	doc := pdftest.Doc{Info: map[string]string{"Title": "/NotAString", "Author": "42"}}

	b := open(t, pdftest.Build(t, doc))
	if b.Title != "" || len(b.Authors) != 0 {
		t.Errorf("Title = %q, Authors = %q, want both empty", b.Title, b.Authors)
	}
}

func TestOpenWithNoMetadata(t *testing.T) {
	eachLayout(t, pdftest.Doc{}, func(t *testing.T, path string) {
		b := open(t, path)
		if !equal(b.Metadata, pdf.Metadata{}) || b.CreationDate() != "" {
			t.Errorf("got %+v, created %q, want nothing", b.Metadata, b.CreationDate())
		}
	})
}

func TestCreationDateIsReportedAsWritten(t *testing.T) {
	info := pdftest.Doc{Info: map[string]string{"CreationDate": "(D:20140314124211+01'00)"}}
	if got := open(t, pdftest.Build(t, info)).CreationDate(); got != "D:20140314124211+01'00" {
		t.Errorf("from the dictionary: %q", got)
	}

	both := info
	both.XMP = pdftest.Packet(`<xmp:CreateDate>2014-03-14T12:42:11+01:00</xmp:CreateDate>`)

	if got := open(t, pdftest.Build(t, both)).CreationDate(); got != "2014-03-14T12:42:11+01:00" {
		t.Errorf("from XMP: %q", got)
	}
}

// A broken packet does not hide the rest of the metadata.
func TestOpenIgnoresAnUnparseablePacket(t *testing.T) {
	doc := pdftest.Doc{Info: map[string]string{"Title": "(Info Title)"}, XMP: "<x:xmpmeta><unclosed>"}

	if got := open(t, pdftest.Build(t, doc)).Title; got != "Info Title" {
		t.Errorf("Title = %q", got)
	}
}

func TestOpenRejectsWhatIsNotAPDF(t *testing.T) {
	valid := pdftest.Bytes(pdftest.Doc{Info: map[string]string{"Title": "(T)"}})

	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"text", []byte("hello, world\n")},
		{"no startxref", []byte(strings.ReplaceAll(string(valid), "startxref", "startxrex"))},
		{"startxref past the end", []byte(strings.Replace(string(valid), "startxref\n", "startxref\n9", 1))},
		{"Prev loops", []byte(strings.Replace(string(valid), "<</Size", "<</Prev "+startxref(valid)+" /Size", 1))},
		{"truncated", valid[:len(valid)/2]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bad.pdf")
			if err := os.WriteFile(path, tc.data, 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := pdf.Open(path); !errors.Is(err, pdf.ErrNotPDF) {
				t.Errorf("Open err = %v, want ErrNotPDF", err)
			}
		})
	}
}

func startxref(pdfBytes []byte) string {
	s := string(pdfBytes)
	s = s[strings.LastIndex(s, "startxref")+len("startxref\n"):]

	return s[:strings.IndexByte(s, '\n')]
}

func TestOpenPassesThroughFileErrors(t *testing.T) {
	_, err := pdf.Open(filepath.Join(t.TempDir(), "missing.pdf"))
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, pdf.ErrNotPDF) {
		t.Errorf("Open err = %v, want ErrNotExist alone", err)
	}
}

// Without a decryption key the dictionary's strings are ciphertext, so Open
// refuses rather than report them. The file structure is not encrypted.
func TestOpenRefusesAnEncryptedPDF(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{Info: map[string]string{"Title": "(T)"}, Trailer: "/Encrypt <</Filter /Standard /V 2>>"})

	if _, err := pdf.Open(path); !errors.Is(err, pdf.ErrEncrypted) {
		t.Errorf("Open err = %v, want ErrEncrypted", err)
	}

	f, err := pdf.OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}

	f.Close()
}

func TestCloseTwiceAndReadAfterClose(t *testing.T) {
	f, err := pdf.OpenFile(pdftest.Build(t, pdftest.Doc{}))
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 5)
	if _, err := f.ReadAt(buf, 0); err != nil || string(buf) != "%PDF-" {
		t.Fatalf("ReadAt = %q, %v", buf, err)
	}

	if err := f.Close(); err != nil || !f.Closed() {
		t.Fatalf("Close = %v, Closed = %v", err, f.Closed())
	}

	if err := f.Close(); !errors.Is(err, pdf.ErrClosed) {
		t.Errorf("second Close = %v, want ErrClosed", err)
	}

	if _, err := f.ReadAt(buf, 0); !errors.Is(err, pdf.ErrClosed) {
		t.Errorf("ReadAt after Close = %v, want ErrClosed", err)
	}
}

// equal treats a nil slice and an empty one as the same, as Save does.
func equal(a, b pdf.Metadata) bool {
	norm := func(m pdf.Metadata) pdf.Metadata {
		for _, f := range reflect.ValueOf(&m).Elem().Fields() {
			if f.Kind() == reflect.Slice && f.Len() == 0 {
				f.SetZero()
			}
		}

		return m
	}

	return reflect.DeepEqual(norm(a), norm(b))
}

// A million "[" must fail cleanly, not exhaust the stack.
func TestOpenSurvivesHostileNesting(t *testing.T) {
	path := pdftest.Build(t, pdftest.Doc{Info: map[string]string{"Title": strings.Repeat("[", 1<<20)}})

	if _, err := pdf.Open(path); !errors.Is(err, pdf.ErrNotPDF) {
		t.Errorf("Open err = %v, want ErrNotPDF", err)
	}
}

// §7.3.5: a name may spell any byte as #xx, so /Ti#74le is /Title. A "#"
// without two digits after it is kept.
func TestOpenDecodesNameEscapes(t *testing.T) {
	doc := pdftest.Doc{Info: map[string]string{"Ti#74le": "(Escaped)", "Author": "/A#Z#4"}}

	b := open(t, pdftest.Build(t, doc))
	if b.Title != "Escaped" {
		t.Errorf("Title = %q, want the entry under the escaped key", b.Title)
	}
}

// An object stream's N is whatever the file claims. Every N beyond the pairs
// its header holds fails, whether it exceeds what First leaves room for (10
// here) or the decoded data too (1000), and before allocating for it.
func TestOpenRejectsAnObjectStreamClaimingTooManyObjects(t *testing.T) {
	valid := pdftest.Bytes(pdftest.Doc{Layout: pdftest.Stream, Info: map[string]string{"Title": "(T)"}})
	if !bytes.Contains(valid, []byte("/ObjStm /N 4 ")) {
		t.Fatal("fixture no longer has a four-object stream")
	}

	for _, n := range []string{"5", "10", "1000", "1000000000000"} {
		t.Run(n, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "doc.pdf")
			if err := os.WriteFile(path, bytes.Replace(valid, []byte("/ObjStm /N 4 "), []byte("/ObjStm /N "+n+" "), 1), 0o644); err != nil {
				t.Fatal(err)
			}

			if _, err := pdf.Open(path); !errors.Is(err, pdf.ErrNotPDF) {
				t.Errorf("Open err = %v, want ErrNotPDF", err)
			}
		})
	}
}
