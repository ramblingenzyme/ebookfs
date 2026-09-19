// The archive under the metadata: the mimetype, the OCF container, which
// rootfile is the package document, and what a rewrite copies through. These
// drive the seam every entry point shares, so a read and the write that follows
// it cannot resolve a name differently.

package epub_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/epub"
	"github.com/ramblingenzyme/ebookfs/internal/epubtest"
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

// --- cover resolution ---

// §4.2.6.3.1.3 makes full-path a path-relative-scheme-less-URL, so "OEBPS/My Book.opf"
// is declared "My%20Book.opf" while the entry holds the decoded name.
// Undecoded, the book is unopenable and blamed on a rootfile that is present.
func TestOpenResolvesEncodedRootfilePath(t *testing.T) {
	container := epubtest.ContainerFor("OEBPS/My%20Book.opf", epubtest.PackageMediaType)

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(container)},
		{Name: "OEBPS/My Book.opf", Data: []byte(epubtest.OPF3)}, // literal space in the entry name
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

// encoding/xml does not apply XML 1.0 §3.3.3 normalization. §4.2.6.3.1.3
// requires the package media type, so a container wrapping it has every
// rootfile skipped and reports ErrNoRootfile for a package that is right there.
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

// Badly repacked epubs carry two entries under one name. Disagreeing means an
// edit computed from one copy and reported from the other, invisible until the
// copies differ. Either rule would do; it has to be one rule.
func TestOpenAndSaveAgreeOnADuplicateEntry(t *testing.T) {
	first := strings.Replace(string(epubtest.OPF3), "Original Title", "First Copy", 1)
	second := strings.Replace(string(epubtest.OPF3), "Original Title", "Second Copy", 1)

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(first)},
		{Name: "OEBPS/content.opf", Data: []byte(second)}, // same name, different content
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "First Copy" {
		t.Errorf("title = %q, want First Copy — Parse must resolve it the way findEntry does", bib.Title)
	}

	// The edit is computed from whichever copy the writer reads, so the re-parse
	// sees the result only when both picked the same one.
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

// %20 decodes, but url.Parse would read "C:/..." as a scheme and truncate at
// '#' or '?'. PathUnescape touches nothing but the escapes. The literal rows
// cover an entry whose name really contains '%20'.
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

// --- container & mimetype validation ---

// Each entry point opens the file itself, so each could grow its own rule.
// Callers tell "not a book" from "the disk is broken" with errors.Is on
// ErrNotEpub. A nonexistent path says nothing about contents, so labelling it
// ErrNotEpub is the obvious wrong fix.
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
	// A trailing newline on the mimetype is tolerated (trimmed), matching calibre.
	p := epubtest.WriteEpub(t, withMimetype(epubtest.BaseEntries(epubtest.OPF3), "application/epub+zip\n"))
	if _, err := parse(t, p); err != nil {
		t.Fatalf("Parse rejected a whitespace-padded mimetype: %v", err)
	}
}

// Lookup is first-wins, but writeUpdatedEpub matches its replacement map by
// name against every entry copied, so both copies of a duplicated package
// document were overwritten. The copy nobody resolved is somebody else's data,
// and the archive is copied verbatim.
func TestSaveReplacesOnlyTheResolvedDuplicate(t *testing.T) {
	first := strings.Replace(string(epubtest.OPF3), "Original Title", "First Copy", 1)
	second := strings.Replace(string(epubtest.OPF3), "Original Title", "Second Copy", 1)

	path := epubtest.WriteEpub(t, []epubtest.Entry{
		{Name: "mimetype", Data: []byte(epubtest.MimetypeValue), Store: true},
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(first)},
		{Name: "OEBPS/content.opf", Data: []byte(second)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	})

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

// A container naming a package document that cannot be found is not a container
// naming none. The two send a reader to different places.
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
			// No builder: an empty <rootfiles> is the shape being tested, not
			// a value inside one.
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

// Kobo epubs sometimes declare several <rootfile> entries where only one
// exists; the absent ones must be skipped on both the read and write paths.
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
		t.Fatalf("writeBib failed on Kobo multi-rootfile epub: %v", err)
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

	// Untouched entries copied verbatim.
	got, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/chapter1.xhtml")
	if !ok || !bytes.Equal(got, epubtest.ChapterBytes) {
		t.Errorf("chapter bytes changed: %q", got)
	}
	cover, ok := epubtest.ReadEntryFromFile(t, path, "OEBPS/cover.jpg")
	if !ok || !bytes.Equal(cover, epubtest.CoverBytes) {
		t.Errorf("cover bytes changed by a metadata-only edit")
	}
}

// OCF requires exactly one mimetype entry, first and stored, so a source with
// two is malformed and copying both preserves the defect the hoist exists to fix.
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

// OCF §4.3.3 layout, asserted as bytes: sniffers read "mimetype" at offset 30
// and its content at 38, so one check covers position, STORED and the MUST NOT
// on extra fields. Both input orders run, or the hoist could be deleted with
// nothing failing.
func TestSaveHoistsMimetypeToTheFront(t *testing.T) {
	rest := []epubtest.Entry{
		{Name: "META-INF/container.xml", Data: []byte(epubtest.ContainerXML)},
		{Name: "OEBPS/content.opf", Data: []byte(epubtest.OPF3)},
		{Name: "OEBPS/cover.jpg", Data: epubtest.CoverBytes},
		{Name: "OEBPS/chapter1.xhtml", Data: epubtest.ChapterBytes},
	}
	mimetype := epubtest.Entry{Name: epubtest.MimetypePath, Data: []byte(epubtest.MimetypeValue), Store: true}

	for _, tc := range []struct {
		name    string
		entries []epubtest.Entry
		// conformingInput says the fixture already satisfies §4.3.3, so the
		// layout is asserted before the write as well. Without that precondition
		// this row proves nothing: a fixture that did not conform would come out
		// conforming anyway, and the assertion would pass without showing that
		// the input order was preserved rather than overridden.
		conformingInput bool
	}{
		{name: "mimetype last", entries: append(slices.Clone(rest), mimetype)},
		{name: "mimetype already first", entries: append([]epubtest.Entry{mimetype}, rest...), conformingInput: true},
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

// assertOCFHeader checks the byte layout OCF §4.3.3 guarantees: the local file
// header first, "mimetype" as its name at offset 30, and the media type
// immediately after at 38.
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
}

// encoding/xml does not normalize attribute values. A wrapped Algorithm must
// still read as font obfuscation, a wrapped URI must still identify the entry.
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

	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	dirs := make(map[string]bool)
	for _, f := range zrc.File {
		if len(f.Name) > 0 && f.Name[len(f.Name)-1] == '/' {
			dirs[f.Name] = true
		}
	}
	for _, dir := range []string{"OEBPS/", "fonts/", "images/", "text/"} {
		if !dirs[dir] {
			t.Errorf("directory entry %q missing from rewritten epub", dir)
		}
	}
}

func TestSetCoverWithDirectoryEntries(t *testing.T) {
	entries := epubtest.BaseEntries(epubtest.OPF3,
		epubtest.Entry{Name: "OEBPS/", Data: nil},
		epubtest.Entry{Name: "fonts/", Data: nil},
	)
	path := epubtest.WriteEpub(t, entries)
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

	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	dirs := make(map[string]bool)
	for _, f := range zrc.File {
		if len(f.Name) > 0 && f.Name[len(f.Name)-1] == '/' {
			dirs[f.Name] = true
		}
	}
	for _, dir := range []string{"OEBPS/", "fonts/"} {
		if !dirs[dir] {
			t.Errorf("directory entry %q missing from rewritten epub", dir)
		}
	}
}

// --- WriteCover ---
