// Package epub_test drives the package from the outside, through Rewrite and
// Parse only. The internal tests reach for editOPF and the element helpers,
// which pins them to how the OPF is written today; these pin what an edit is
// allowed to do to a file, which is the part that must not change.
//
// The rules under test:
//
//   - an edit preserves every piece of metadata it was not asked to change,
//     including metadata ebookfs did not write and does not understand;
//   - an edit round-trips, i.e. Parse reads back what the edit asked for;
//   - an edit is idempotent, so applying it twice does not accumulate
//     duplicates or churn the file further.
package epub_test

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

const opfPath = "OEBPS/content.opf"

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
			})
		}
	}
}

func TestRewriteRoundTrips(t *testing.T) {
	s := func(v string) *string { return &v }
	authors := []model.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}, {Name: "Bo Li"}}

	cases := []struct {
		name  string
		e     model.Edits
		check func(*testing.T, model.Bib)
	}{
		{"title", model.Edits{Title: s("New Title")}, func(t *testing.T, b model.Bib) {
			if b.Title != "New Title" {
				t.Errorf("title = %q", b.Title)
			}
			// Documented rule: a title change without a new sort title clears
			// the old one, which described the old title.
			if b.SortTitle != "" {
				t.Errorf("sort title = %q, want cleared by the title change", b.SortTitle)
			}
		}},
		{"sort title", model.Edits{SortTitle: s("Title, New")}, func(t *testing.T, b model.Bib) {
			if b.SortTitle != "Title, New" {
				t.Errorf("sort title = %q", b.SortTitle)
			}
		}},
		{"description", model.Edits{Description: s("A new description.")}, func(t *testing.T, b model.Bib) {
			if b.Description != "A new description." {
				t.Errorf("description = %q", b.Description)
			}
		}},
		{"language", model.Edits{Language: s("fr")}, func(t *testing.T, b model.Bib) {
			if b.Language != "fr" {
				t.Errorf("language = %q", b.Language)
			}
		}},
		{"authors", model.Edits{Authors: &authors}, func(t *testing.T, b model.Bib) {
			if len(b.Authors) != 2 || b.Authors[0].Name != "Ann Rand" || b.Authors[1].Name != "Bo Li" {
				t.Fatalf("authors = %+v", b.Authors)
			}
			if b.Authors[0].SortName != "Rand, Ann" || b.Authors[1].SortName != "" {
				t.Errorf("sort names = %q, %q", b.Authors[0].SortName, b.Authors[1].SortName)
			}
		}},
		{"series rename keeps position", model.Edits{Series: s("The Quartet")}, func(t *testing.T, b model.Bib) {
			if b.Series == nil || b.Series.Name != "The Quartet" || b.Series.Index != "2" {
				t.Errorf("series = %+v, want The Quartet at 2", b.Series)
			}
		}},
		{"series index keeps name", model.Edits{SeriesIndex: s("4")}, func(t *testing.T, b model.Bib) {
			if b.Series == nil || b.Series.Name != "The Trilogy" || b.Series.Index != "4" {
				t.Errorf("series = %+v, want The Trilogy at 4", b.Series)
			}
		}},
		{"series cleared", model.Edits{Series: s("")}, func(t *testing.T, b model.Bib) {
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

// TestRewriteDiscardsEPUB2SortTitle pins a known gap: EPUB 2 has no standard
// title-sort mechanism, and ebookfs writes no proprietary fallback the way it
// writes calibre:series for the series, so the edit is accepted and silently
// dropped. Nothing under fs/ sets SortTitle, so only a programmatic Library
// caller can reach it.
//
// Unresolved on purpose — it needs a decision, not a fix: write
// calibre:title_sort the way calibre does, or refuse loudly so the caller gets
// an error instead of a lie. Delete this test whichever way that goes.
func TestRewriteDiscardsEPUB2SortTitle(t *testing.T) {
	path := buildEpub(t, richOPF2)
	want := "Title, New"
	bib, err := epub.Rewrite(path, book(t, path), model.Edits{SortTitle: &want})
	if err != nil {
		t.Fatalf("the edit is accepted, not refused: %v", err)
	}
	if bib.SortTitle != "" {
		t.Errorf("sort title = %q; EPUB 2 sort titles now round-trip, delete this test", bib.SortTitle)
	}
}

// TestRewriteIsIdempotent applies the same edit twice. The second write must
// produce byte-identical OPF: anything else means the writer appends where it
// should replace, and repeated edits would grow the file or reorder it forever.
func TestRewriteIsIdempotent(t *testing.T) {
	for _, c := range corpora() {
		for _, tc := range preservingEdits() {
			t.Run(c.name+"/"+tc.name, func(t *testing.T) {
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
		}
	}
}

// --- helpers -----------------------------------------------------------------

// book builds the Book that Rewrite validates against, the way library.Edit
// does: from the file's current state. An index edit is refused unless the book
// already has a series, so an empty Bib is not a usable stand-in.
func book(t *testing.T, path string) *model.Book {
	t.Helper()
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	return &model.Book{Location: model.Location{EpubPath: path}, Bib: *bib}
}

const containerXML = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`

func buildEpub(t *testing.T, opf string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.epub")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	zw := zip.NewWriter(f)
	for _, e := range []struct {
		name  string
		data  string
		store bool
	}{
		{"mimetype", "application/epub+zip", true},
		{"META-INF/container.xml", containerXML, false},
		{opfPath, opf, false},
		{"OEBPS/cover.jpg", "COVER-BYTES", false},
		{"OEBPS/chapter1.xhtml", "<html><body><p>one</p></body></html>", false},
	} {
		method := zip.Deflate
		if e.store {
			method = zip.Store
		}
		w, err := zw.CreateHeader(&zip.FileHeader{Name: e.name, Method: method})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func readEntry(t *testing.T, path, name string) []byte {
	t.Helper()
	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	for _, f := range zrc.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	t.Fatalf("entry %q not found", name)
	return nil
}
