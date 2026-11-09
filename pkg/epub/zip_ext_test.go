package epub_test

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

func withContainer(entries []epubtest.Entry, container string) []epubtest.Entry {
	out := make([]epubtest.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		if out[i].Name == "META-INF/container.xml" {
			out[i].Data = []byte(container)
		}
	}
	return out
}

func withMimetype(entries []epubtest.Entry, value string) []epubtest.Entry {
	out := make([]epubtest.Entry, len(entries))
	copy(out, entries)
	for i := range out {
		if out[i].Name == "mimetype" {
			out[i].Data = []byte(value)
		}
	}
	return out
}

func withoutEntry(entries []epubtest.Entry, name string) []epubtest.Entry {
	var out []epubtest.Entry
	for _, e := range entries {
		if e.Name != name {
			out = append(out, e)
		}
	}
	return out
}

// §4.2.6.3.1.3 makes full-path a URL, so a space in the entry name is declared
// as %20.
func TestOpenResolvesEncodedRootfilePath(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/My%20Book.opf", epubtest.PackageMediaType)

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(container)},
		{Name: "OEBPS/My Book.opf", Data: []byte(epubtest.OPF3)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	if _, err := parse(t, path); err != nil {
		t.Fatalf("Parse failed for a percent-encoded full-path: %v", err)
	}
	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Another Title" }); err != nil {
		t.Fatalf("edit failed for a percent-encoded full-path: %v", err)
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Another Title" {
		t.Errorf("title = %q, want the edit to have landed in the resolved package document", bib.Title)
	}
}

// encoding/xml does not apply XML 1.0 §3.3.3 whitespace normalisation, so a
// padded media type would match no rootfile.
func TestOpenCollapsesRootfileMediaType(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/content.opf", epubtest.Wrapped(epubtest.PackageMediaType))

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(container)},
		{Name: "OEBPS/content.opf", Data: []byte(epubtest.OPF3)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	if _, err := parse(t, path); err != nil {
		t.Fatalf("Parse failed for a padded rootfile media-type: %v", err)
	}
}

// Either copy would do, as long as Open and Save pick the same one.
func TestOpenAndSaveAgreeOnADuplicateEntry(t *testing.T) {
	path := duplicateOPFEpub(t)

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "First Copy" {
		t.Errorf("title = %q, want First Copy, the copy Save resolves", bib.Title)
	}

	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Edited Title" }); err != nil {
		t.Fatal(err)
	}
	bib, err = parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Edited Title" {
		t.Errorf("title = %q, want the edit to be visible to the next read", bib.Title)
	}
}

// url.Parse would read "C:/..." as a scheme and cut at '#' or '?', so only the
// escapes are decoded.
func TestOpenRootfilePathEdgeCases(t *testing.T) {
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
			container := epubtest.ContainerFor(tc.declared, epubtest.PackageMediaType)

			path := epubtest.WriteEpub(t, []epubtest.Entry{
				{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
				{Name: "META-INF/container.xml", Data: []byte(container)},
				{Name: tc.entry, Data: []byte(epubtest.OPF3)},
				{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
				{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
			})

			if _, err := parse(t, path); err != nil {
				t.Fatalf("full-path %q naming entry %q: %v", tc.declared, tc.entry, err)
			}
		})
	}
}

// Callers use ErrNotEpub to tell a bad book from a broken disk, so a missing
// file keeps its os error.
func TestEntryPointsAgreeOnABadEpub(t *testing.T) {
	good := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	raw, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	write := func(name string, data []byte) string {
		p := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	for _, tc := range []struct {
		name    string
		path    string
		notEpub bool // ErrNotEpub, rather than the os error verbatim
	}{
		{name: "not a zip", path: write("junk.epub", []byte("this is plainly not a zip archive")), notEpub: true},
		{name: "empty file", path: write("empty.epub", nil), notEpub: true},
		{name: "truncated epub", path: write("trunc.epub", raw[:len(raw)/2]), notEpub: true},
		{name: "missing file", path: filepath.Join(t.TempDir(), "nope.epub")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, openErr := parse(t, tc.path)
			_, fileErr := epub.OpenFile(tc.path)
			_, saveErr := save(t, tc.path, func(b *epub.Book) { b.Title = "X" })

			for _, e := range []struct {
				from string
				err  error
			}{{"Open", openErr}, {"OpenFile", fileErr}, {"Save", saveErr}} {
				if e.err == nil {
					t.Errorf("%s returned no error", e.from)
					continue
				}
				if got := errors.Is(e.err, epub.ErrNotEpub); got != tc.notEpub {
					t.Errorf("%s: errors.Is(err, ErrNotEpub) = %v, want %v (err = %v)",
						e.from, got, tc.notEpub, e.err)
				}
				if !tc.notEpub && !errors.Is(e.err, os.ErrNotExist) {
					t.Errorf("%s: err = %v, want the os error to survive", e.from, e.err)
				}
			}
		})
	}
}

func TestOpenRejectsNonZip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "junk.epub")
	if err := os.WriteFile(p, []byte("this is plainly not a zip archive"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := parse(t, p); !errors.Is(err, epub.ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestOpenReportsMissingFile(t *testing.T) {
	if _, err := parse(t, filepath.Join(t.TempDir(), "absent.epub")); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestOpenRejectsWrongMimetype(t *testing.T) {
	p := epubtest.WriteEpub(t, withMimetype(epubtest.BaseEntries(epubtest.OPF3), "application/zip"))
	if _, err := parse(t, p); !errors.Is(err, epub.ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestOpenRejectsMissingMimetype(t *testing.T) {
	p := epubtest.WriteEpub(t, withoutEntry(epubtest.BaseEntries(epubtest.OPF3), "mimetype"))
	if _, err := parse(t, p); !errors.Is(err, epub.ErrNotEpub) {
		t.Fatalf("err = %v, want ErrNotEpub", err)
	}
}

func TestOpenToleratesMimetypeWhitespace(t *testing.T) {
	// calibre also trims it.
	p := epubtest.WriteEpub(t, withMimetype(epubtest.BaseEntries(epubtest.OPF3), "application/epub+zip\n"))
	if _, err := parse(t, p); err != nil {
		t.Fatalf("Parse rejected a whitespace-padded mimetype: %v", err)
	}
}

func TestSaveReplacesOnlyTheResolvedDuplicate(t *testing.T) {
	path := duplicateOPFEpub(t)

	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Edited Title" }); err != nil {
		t.Fatal(err)
	}

	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()

	var copies []string
	for _, f := range zrc.File {
		if f.Name != "OEBPS/content.opf" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case bytes.Contains(b, []byte("Edited Title")):
			copies = append(copies, "edited")
		case bytes.Contains(b, []byte("Second Copy")):
			copies = append(copies, "second, untouched")
		default:
			copies = append(copies, "other")
		}
	}
	if want := []string{"edited", "second, untouched"}; !slices.Equal(copies, want) {
		t.Errorf("content.opf copies = %v, want %v", copies, want)
	}
}

func TestOpenDistinguishesMissingFromUndeclared(t *testing.T) {
	for _, tc := range []struct {
		name, container string
		want            error
	}{
		{
			name:      "declared but absent from the archive",
			container: epubtest.ContainerFor("OEBPS/nowhere.opf", epubtest.PackageMediaType),
			want:      epub.ErrRootfileMissing,
		},
		{
			name:      "no rootfile of the package media type",
			container: epubtest.ContainerFor("OEBPS/content.opf", "application/x-something-else"),
			want:      epub.ErrNoRootfile,
		},
		{
			name: "no rootfiles at all",
			// ContainerFor cannot build an empty <rootfiles>.
			container: `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles></rootfiles>
</container>`,
			want: epub.ErrNoRootfile,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, withContainer(epubtest.BaseEntries(epubtest.OPF3), tc.container))
			_, err := parse(t, path)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestMultipleRootfilesKobo(t *testing.T) {
	path := epubtest.WriteEpub(t, withContainer(epubtest.BaseEntries(epubtest.OPF3), epubtest.MultiRootContainer))

	book, err := parse(t, path)
	if err != nil {
		t.Fatalf("Parse failed on Kobo multi-rootfile epub: %v", err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title", book.Title)
	}

	edited, err := save(t, path, func(b *epub.Book) { b.Title = "Edited Title" })
	if err != nil {
		t.Fatalf("Save failed on Kobo multi-rootfile epub: %v", err)
	}
	if edited.Title != "Edited Title" {
		t.Errorf("edited title = %q, want Edited Title", edited.Title)
	}
}

func TestSavePreservesContainerLayout(t *testing.T) {
	path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3))
	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Another Title" }); err != nil {
		t.Fatal(err)
	}

	got, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/chapter1.xhtml")
	if !ok || !bytes.Equal(got, epubtest.ChapterBytes) {
		t.Errorf("chapter bytes changed: %q", got)
	}
	cover, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/cover.jpg")
	if !ok || !bytes.Equal(cover, epubtest.CoverBytes) {
		t.Errorf("cover bytes changed by a metadata-only edit")
	}
}

// OCF allows one mimetype entry, so copying both would keep the file
// malformed.
func TestSaveDeduplicatesMimetype(t *testing.T) {
	mt := epubtest.Entry{Name: epubtest.MimetypePath, Data: []byte(epubtest.MimetypeValue), Store: true}
	entries := append([]epubtest.Entry{mt}, epubtest.BaseEntries(epubtest.OPF3)[1:]...)
	entries = append(entries, mt)

	path := epubtest.WriteEpub(t, entries)
	if _, err := save(t, path, func(b *epub.Book) { b.Title = "Another Title" }); err != nil {
		t.Fatal(err)
	}

	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()

	var n int
	for _, f := range zrc.File {
		if f.Name == epubtest.MimetypePath {
			n++
		}
	}
	if n != 1 {
		t.Errorf("mimetype entries = %d, want exactly one", n)
	}
	assertOCFHeader(t, path, "after the write")
}

// OCF §4.3.3: sniffers read "mimetype" at offset 30 and its content at 38, so
// the header must be stored, carry no extra field, and hold exactly the media
// type. Save writes it that way whatever the input did. Both input orders run,
// or writing mimetype first could be deleted with nothing failing.
func TestSaveWritesAConformingMimetype(t *testing.T) {
	rest := []epubtest.Entry{
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(epubtest.OPF3)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	}
	mimetype := epubtest.Entry{Name: epubtest.MimetypePath, Data: []byte(epubtest.MimetypeValue), Store: true}
	deflated := mimetype
	deflated.Store = false
	padded := mimetype
	padded.Data = []byte(epubtest.MimetypeValue + "\n")

	for _, tc := range []struct {
		name    string
		entries []epubtest.Entry
		// conformingInput also checks the fixture before the write. Without it,
		// the row passes even if the fixture never conformed.
		conformingInput bool
	}{
		{name: "mimetype last", entries: append(slices.Clone(rest), mimetype)},
		{name: "mimetype already first", entries: append([]epubtest.Entry{mimetype}, rest...), conformingInput: true},
		{name: "mimetype deflated", entries: append([]epubtest.Entry{deflated}, rest...)},
		{name: "mimetype with a trailing newline", entries: append([]epubtest.Entry{padded}, rest...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.WriteEpub(t, tc.entries)
			if tc.conformingInput {
				assertOCFHeader(t, path, "before the write")
			}

			if _, err := save(t, path, func(b *epub.Book) { b.Title = "Another Title" }); err != nil {
				t.Fatal(err)
			}
			assertOCFHeader(t, path, "after the write")
		})
	}
}

func assertOCFHeader(t *testing.T, path, when string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	end := 38 + len(epubtest.MimetypeValue)
	if len(raw) < end {
		t.Fatalf("%s: archive is %d bytes, too short to carry an OCF header", when, len(raw))
	}
	if !bytes.HasPrefix(raw, []byte("PK\x03\x04")) ||
		string(raw[30:38]) != epubtest.MimetypePath ||
		string(raw[38:end]) != epubtest.MimetypeValue {
		t.Errorf("%s: OCF header = %q, want mimetype at offset 30 and %q at 38", when, raw[:end], epubtest.MimetypeValue)
	}
	// Local file header fields, APPNOTE §4.3.7. The size comes from the
	// central directory, since a streamed entry leaves it zero here.
	method := binary.LittleEndian.Uint16(raw[8:])
	extra := binary.LittleEndian.Uint16(raw[28:])
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("%s: %v", when, err)
	}
	size := zr.File[0].UncompressedSize64
	if method != zip.Store || extra != 0 || size != uint64(len(epubtest.MimetypeValue)) {
		t.Errorf("%s: mimetype method %d, extra %d bytes, size %d; want stored, none, %d",
			when, method, extra, size, len(epubtest.MimetypeValue))
	}
}

// encoding/xml does not normalise attribute whitespace, so both attributes are
// collapsed by hand.
func TestEncryptionAttributesAreCollapsed(t *testing.T) {
	t.Run("wrapped obfuscation algorithm still allows the edit", func(t *testing.T) {
		enc := epubtest.EncryptionXML(epubtest.Wrapped(epubtest.FontObfusc), "OEBPS/fonts/x.otf")
		path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3,
			epubtest.Entry{Name: "META-INF/encryption.xml", Data: []byte(enc)},
			epubtest.Entry{Name: "OEBPS/fonts/x.otf", Data: []byte("obfuscated")},
		))
		if _, err := save(t, path, func(b *epub.Book) { b.Title = "Fine" }); err != nil {
			t.Errorf("font obfuscation with a wrapped algorithm blocked the edit: %v", err)
		}
	})

	t.Run("wrapped URI still identifies the encrypted OPF", func(t *testing.T) {
		enc := epubtest.EncryptionXML(epubtest.AES256, epubtest.Wrapped("OEBPS/content.opf"))
		path := epubtest.WriteEpub(t, epubtest.BaseEntries(epubtest.OPF3, epubtest.Entry{Name: "META-INF/encryption.xml", Data: []byte(enc)}))
		if _, err := save(t, path, func(b *epub.Book) { b.Title = "Hack" }); err == nil {
			t.Error("edited an encrypted OPF whose URI was wrapped")
		}
	})
}

func TestSaveWithDirectoryEntries(t *testing.T) {
	entries := epubtest.BaseEntries(epubtest.OPF3,
		epubtest.Entry{Name: "OEBPS/", Data: nil},
		epubtest.Entry{Name: "fonts/", Data: nil},
		epubtest.Entry{Name: "images/", Data: nil},
		epubtest.Entry{Name: "text/", Data: nil},
	)
	path := epubtest.WriteEpub(t, entries)
	book, err := save(t, path, func(b *epub.Book) { b.Title = "New Title" })
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "New Title" {
		t.Errorf("title = %q, want New Title", book.Title)
	}

	wantDirEntries(t, path, "OEBPS/", "fonts/", "images/", "text/")
}

func TestSetCoverWithDirectoryEntries(t *testing.T) {
	entries := epubtest.BaseEntries(epubtest.OPF3,
		epubtest.Entry{Name: "OEBPS/", Data: nil},
		epubtest.Entry{Name: "fonts/", Data: nil},
	)
	path := epubtest.WriteEpub(t, entries)
	swapCover(t, path)

	wantDirEntries(t, path, "OEBPS/", "fonts/")
}

// One byte past the 32 MiB cap on a decompressed entry. Deflated zeros keep
// the fixture a few kilobytes on disk, the shape a zip bomb takes.
var overCap = bytes.Repeat([]byte{0}, 32<<20+1)

func TestReadRefusesAnEntryOverTheCap(t *testing.T) {
	entries := epubtest.BaseEntries(epubtest.OPF3)
	for i := range entries {
		if entries[i].Name == "OEBPS/cover.jpg" {
			entries[i].Data = overCap
		}
	}
	b := open(t, epubtest.WriteEpub(t, entries))

	if _, err := b.Cover(); err == nil || !strings.Contains(err.Error(), "over the") {
		t.Errorf("Cover() err = %v, want the size limit", err)
	}
	if _, err := b.ReadEntry("OEBPS/cover.jpg"); err == nil {
		t.Error("ReadEntry read an entry over the cap")
	}
}

// mimetype is read on every open, so an oversized one fails Open itself. The
// padding is whitespace, which validate trims, so without the cap this file
// opens after inflating every byte.
func TestOpenRefusesAMimetypeOverTheCap(t *testing.T) {
	padded := append([]byte(epubtest.MimetypeValue), bytes.Repeat([]byte(" "), len(overCap))...)
	entries := epubtest.BaseEntries(epubtest.OPF3)
	for i := range entries {
		if entries[i].Name == epubtest.MimetypePath {
			entries[i] = epubtest.Entry{Name: epubtest.MimetypePath, Data: padded}
		}
	}
	if _, err := parse(t, epubtest.WriteEpub(t, entries)); err == nil || !strings.Contains(err.Error(), "over the") {
		t.Errorf("Open err = %v, want the size limit", err)
	}
}
