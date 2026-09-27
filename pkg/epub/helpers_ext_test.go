package epub_test

import (
	"archive/zip"
	"bytes"
	"image"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

func open(t *testing.T, path string) *epub.Book {
	t.Helper()
	b, err := epub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

func parse(t *testing.T, path string) (*epub.Book, error) {
	t.Helper()
	b, err := epub.Open(path)
	if b != nil {
		t.Cleanup(func() { b.Close() })
	}
	return b, err
}

func save(t *testing.T, path string, edit func(*epub.Book)) (*epub.Book, error) {
	t.Helper()
	b, err := parse(t, path)
	if err != nil {
		return nil, err
	}
	edit(b)
	return b, b.Save()
}

func setCover(t *testing.T, path string, img []byte) (*epub.Book, error) {
	t.Helper()
	b, err := parse(t, path)
	if err != nil {
		return nil, err
	}
	if err := b.SetCover(img); err != nil {
		return nil, err
	}
	return b, b.Save()
}

// rename and reposition change one half of a series and keep the other, as
// the library's edits do.
func rename(b *epub.Book, name string) {
	if name == "" {
		b.Series = nil
		return
	}
	index := ""
	if b.Series != nil {
		index = b.Series.Index
	}
	b.Series = &epub.Series{Name: name, Index: index}
}

func reposition(b *epub.Book, index string) {
	if b.Series == nil {
		return
	}
	b.Series = &epub.Series{Name: b.Series.Name, Index: index}
}

// item is metadata ebookfs never reads or writes, such as an author's
// alternate script, a series ISSN or a calibre column. An edit to anything
// else must leave it exactly where it was. Dropped creator refinements and a
// dropped series identifier both broke that rule.
type item struct {
	name string
	path string // etree path, relative to <metadata>
	text string // expected element text; "" to assert presence only
}

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// coverPageManifest declares a cover page that nothing points at. Each fixture
// below adds one pointer, so a test names the one it exercises.
const coverPageManifest = `<item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="coverpage" href="cover.xhtml" media-type="application/xhtml+xml"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`

var (
	coverPageInGuide = epubtest.Pkg{Manifest: coverPageManifest, Guide: true}.EPUB3()
	coverPageInSpine = epubtest.Pkg{Manifest: coverPageManifest, Spine: "coverpage"}.EPUB3()
)

func authorNames(b *epub.Book) []string {
	var out []string
	for _, a := range b.Authors {
		out = append(out, a.Name)
	}
	return out
}

// §5.5.2: values MUST be non-empty "after leading and trailing ASCII whitespace
// is stripped", and internal runs are "collapsed to a single space during
// processing". A conforming reader does both.

var opfSpecStyleWhitespace = epubtest.EPUB3(`    <dc:identifier id="pub-id">
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
    <meta property="belongs-to-collection" id="c01">
      The New French Cuisine Masters
    </meta>
    <meta refines="#c01" property="collection-type">
      series
    </meta>
    <meta refines="#c01" property="group-position">
      2
    </meta>`)

// A dropped directory entry does not show in the metadata, so it is checked
// directly.
func wantDirEntries(t *testing.T, path string, want ...string) {
	t.Helper()
	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()

	dirs := make(map[string]bool)
	for _, f := range zrc.File {
		if strings.HasSuffix(f.Name, "/") {
			dirs[f.Name] = true
		}
	}
	for _, dir := range want {
		if !dirs[dir] {
			t.Errorf("directory entry %q missing from rewritten epub", dir)
		}
	}
}

func swapCover(t *testing.T, path string) []byte {
	t.Helper()
	newCover := tinyJPEG(t)
	if _, err := setCover(t, path, newCover); err != nil {
		t.Fatal(err)
	}
	got, err := open(t, path).Cover()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newCover) {
		t.Errorf("cover = %q, want the supplied JPEG bytes", got)
	}
	return newCover
}

func saveSortName(t *testing.T, opf epubtest.PackageDoc) string {
	t.Helper()
	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want the sort name the edit asked for", bib.Authors)
	}
	return path
}

// Badly repacked epubs carry two entries with one name like this.
func duplicateOPFEpub(t *testing.T) string {
	t.Helper()
	first := strings.Replace(string(epubtest.OPF3), "Original Title", "First Copy", 1)
	second := strings.Replace(string(epubtest.OPF3), "Original Title", "Second Copy", 1)

	return epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(first)},
		{Name: "OEBPS/content.opf", Data: []byte(second)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})
}
