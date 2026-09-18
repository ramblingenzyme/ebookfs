// The two calls these tests put an edit through, and the reads that check one
// landed. The corpus itself — the archive, the package document, the assertions
// over it — is internal/epubtest, shared with the adapter's suite.

package epub_test

import (
	"bytes"
	"image"
	"image/jpeg"
	"testing"

	"github.com/ramblingenzyme/ebookfs/epub"
	"github.com/ramblingenzyme/ebookfs/internal/epubtest"
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

// save opens the epub, applies edit, and saves it in place, returning the Book
// so a test can read back what the file now says. Production drives this flow
// through library.Edit.
func save(t *testing.T, path string, edit func(*epub.Book)) *epub.Book {
	t.Helper()
	b, err := saveErr(t, path, edit)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// saveErr is save for the tests that are about the refusal rather than the
// result. The Book is returned either way so a caller can still read it.
func saveErr(t *testing.T, path string, edit func(*epub.Book)) (*epub.Book, error) {
	t.Helper()
	b := open(t, path)
	edit(b)
	return b, b.Save()
}

// names is the author names in order, for the tests that are about which
// creators the document yields rather than how they sort.
func names(as []epub.Author) []string {
	out := make([]string, len(as))
	for i, a := range as {
		out[i] = a.Name
	}
	return out
}

// authors builds the Authors slice from name/sort pairs.
func authors(pairs ...string) []epub.Author {
	var out []epub.Author
	for i := 0; i < len(pairs); i += 2 {
		out = append(out, epub.Author{Name: pairs[i], SortName: pairs[i+1]})
	}
	return out
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

// writeBib applies edit to the epub at path and saves it in place, returning the
// Book so a test can read back what the file now says. Production drives this
// flow through library.Edit.
func writeBib(t *testing.T, path string, edit func(*epub.Book)) (*epub.Book, error) {
	t.Helper()
	b, err := parse(t, path)
	if err != nil {
		return nil, err
	}
	edit(b)
	return b, b.Save()
}

// writeCover replaces the cover image and saves. There is no cover path to pass:
// the manifest names the entry, and SetCover replaces that one in place.
//
// SetCover's refusals are returned rather than fatal: four tests here are about
// which images it turns away.
func writeCover(t *testing.T, path string, img []byte) (*epub.Book, error) {
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
