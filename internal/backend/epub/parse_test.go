package epub

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
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
const opfMarkupCoverImage = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
    <meta name="cover" content="real-cover"/>
  </metadata>
  <manifest>
    <item id="coverpage" href="coverpage.xhtml" media-type="application/xhtml+xml" properties="cover-image"/>
    <item id="real-cover" href="cover.jpg" media-type="image/jpeg"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`

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

func TestCoverUrl(t *testing.T) {
	for _, tc := range []struct {
		name, base, href, want string
	}{
		{"relative", "OEBPS", "cover.jpg", "OEBPS/cover.jpg"},
		{"root dir", ".", "cover.jpg", "cover.jpg"},
		{"nested base", "OEBPS/images", "cover.jpg", "OEBPS/images/cover.jpg"},
		{"parent traversal", "OEBPS/text", "../images/cover.jpg", "OEBPS/images/cover.jpg"},
		{"container-absolute", "OEBPS", "/images/cover.jpg", "images/cover.jpg"},
		{"percent-encoded space", "OEBPS", "cover%20image.jpg", "OEBPS/cover image.jpg"},
		{"percent-encoded utf8", "OEBPS", "couv%C3%A9.jpg", "OEBPS/couvé.jpg"},
		{"absolute and encoded", "OEBPS", "/img/a%20b.jpg", "img/a b.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := coverUrl(tc.base, tc.href); got != tc.want {
				t.Errorf("coverUrl(%q, %q) = %q, want %q", tc.base, tc.href, got, tc.want)
			}
		})
	}
}

// A percent-encoded cover href must resolve to the literal zip entry so the
// cover is found by both Parse and the WriteCover/ExtractCover lookups.
func TestParseResolvesEncodedCoverHref(t *testing.T) {
	const opfEncoded = `<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="bookid">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="bookid">urn:uuid:1234</dc:identifier>
    <dc:title>Original Title</dc:title>
    <dc:creator id="creator1">Jane Doe</dc:creator>
    <meta refines="#creator1" property="role">aut</meta>
  </metadata>
  <manifest>
    <item id="cover-img" href="cover%20image.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="ch1"/></spine>
</package>`
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
	got, err := ExtractCover(path, book.CoverPath)
	if err != nil {
		t.Fatalf("ExtractCover failed for an encoded-href cover: %v", err)
	}
	if !bytes.Equal(got, coverBytes) {
		t.Errorf("extracted cover bytes mismatch")
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

	edited, err := WriteBib(path, Edits{Title: ptr("Edited Title")})
	if err != nil {
		t.Fatalf("WriteBib failed on Kobo multi-rootfile epub: %v", err)
	}
	if edited.Title != "Edited Title" {
		t.Errorf("edited title = %q, want Edited Title", edited.Title)
	}
}
