// The two calls these tests put an edit through, and the reads that check one
// landed. The corpus itself — the archive, the package document, the assertions
// over it — is internal/epubtest, shared with the adapter's suite.

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

// open opens the epub and closes it when the test ends.
func open(t *testing.T, path string) *epub.Book {
	t.Helper()
	b, err := epub.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { b.Close() })
	return b
}

var _ = epubtest.OPF3

// parse opens the epub and closes it when the test ends, returning the error
// rather than failing, for the tests that are about a file that will not open.
func parse(t *testing.T, path string) (*epub.Book, error) {
	t.Helper()
	b, err := epub.Open(path)
	if b != nil {
		t.Cleanup(func() { b.Close() })
	}
	return b, err
}

// save applies edit to the epub at path and saves it in place, returning the
// Book so a test can read back what the file now says. Production drives this
// flow through library.Edit.
func save(t *testing.T, path string, edit func(*epub.Book)) (*epub.Book, error) {
	t.Helper()
	b, err := parse(t, path)
	if err != nil {
		return nil, err
	}
	edit(b)
	return b, b.Save()
}

// setCover replaces the cover image and saves. There is no cover path to pass:
// the manifest names the entry, and SetCover replaces that one in place.
//
// SetCover's refusals are returned rather than fatal: four tests here are about
// which images it turns away.
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

// rename changes the series name, keeping the position the book had, and
// removes the membership when name is "". reposition is the same for the other
// half, and does nothing to a book in no series, which has no position to set.
//
// The library names one half of a series at a time and carries the other over.
// The file's own API takes the whole value, so a test changing one half says
// here which half it meant to leave alone.
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

// item is a piece of metadata no part of ebookfs writes, reads, or understands:
// a publisher's alternate script for an author, a series ISSN, a set the book
// belongs to, an editor, a calibre column. An edit that changes something else
// must leave every one of them exactly where it was.
//
// Both bugs this suite exists to catch, dropped creator refinements and a
// dropped series identifier, were failures of that rule.
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

// Both packages declare a cover page in the manifest and point at it exactly
// once, so a test says in its fixture which of the two pointers it exercises.
// coverPageManifest alone points at it from nowhere.
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

// --- whitespace in metadata values ---
//
// §5.5.2: values MUST be non-empty "after leading and trailing ASCII whitespace
// is stripped", and internal runs are "collapsed to a single space during
// processing". A processing step, so a conforming reader does it.

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

// wantDirEntries asserts the rewritten archive still carries a zip entry for
// each named directory. Save copies entries across rather than rebuilding the
// archive, so a dropped directory entry is a regression a reader would not see
// in the book's metadata.
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

// swapCover replaces the cover with a fresh JPEG and returns the bytes it
// wrote, having checked they read back.
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

// saveSortName puts one author with a sort name through Save and asserts the
// parse reads that sort name back. The callers differ only in the package
// document they start from, which is the whole point of each: a stale v2
// attribute, a bare file-as, and so on.
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

// duplicateOPFEpub writes an archive carrying two OEBPS/content.opf entries
// under that one name, titled "First Copy" and "Second Copy" so a test can say
// which copy a read resolved to. Badly repacked epubs look like this.
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
