package epub

import (
	"archive/zip"
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/shared/model"
)

// --- writer-only helpers ---------------------------------------------------

func ptr[T any](v T) *T { return &v }

func tinyJPEG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readEntryFromFile(t *testing.T, path, name string) ([]byte, bool, zip.FileHeader) {
	t.Helper()
	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	for _, f := range zrc.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer rc.Close()
			b := new(bytes.Buffer)
			if _, err := b.ReadFrom(rc); err != nil {
				t.Fatal(err)
			}
			return b.Bytes(), true, f.FileHeader
		}
	}
	return nil, false, zip.FileHeader{}
}

// --- WriteBib --------------------------------------------------------------

func TestWriteBibSimpleFields(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  string
	}{{"epub3", opf3}, {"epub2", opf2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEpub(t, baseEntries(tc.opf))
			book, err := WriteBib(path, model.Edits{
				Title:       ptr("New Title"),
				Description: ptr("New description."),
				Language:    ptr("fr"),
			})
			if err != nil {
				t.Fatal(err)
			}
			if book.Title != "New Title" {
				t.Errorf("title = %q, want New Title", book.Title)
			}
			if book.Description != "New description." {
				t.Errorf("description = %q", book.Description)
			}
			if book.Language != "fr" {
				t.Errorf("language = %q, want fr", book.Language)
			}
			// Re-parse from disk independently to confirm it persisted.
			reparsed, err := Parse(path)
			if err != nil {
				t.Fatal(err)
			}
			if reparsed.Title != "New Title" {
				t.Errorf("persisted title = %q", reparsed.Title)
			}
		})
	}
}

func TestWriteBibPreservesContainerLayout(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := WriteBib(path, model.Edits{Title: ptr("Another Title")}); err != nil {
		t.Fatal(err)
	}

	// mimetype must be first and STORED.
	zrc, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zrc.Close()
	if zrc.File[0].Name != "mimetype" {
		t.Fatalf("first entry = %q, want mimetype", zrc.File[0].Name)
	}
	if zrc.File[0].Method != zip.Store {
		t.Errorf("mimetype method = %d, want Store(%d)", zrc.File[0].Method, zip.Store)
	}

	// Untouched entries copied verbatim.
	got, ok, _ := readEntryFromFile(t, path, "OEBPS/chapter1.xhtml")
	if !ok || !bytes.Equal(got, chapterBytes) {
		t.Errorf("chapter bytes changed: %q", got)
	}
	cover, ok, _ := readEntryFromFile(t, path, "OEBPS/cover.jpg")
	if !ok || !bytes.Equal(cover, coverBytes) {
		t.Errorf("cover bytes changed by a metadata-only edit")
	}
}

func TestWriteBibAuthorsRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  string
	}{{"epub3", opf3}, {"epub2", opf2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEpub(t, baseEntries(tc.opf))
			authors := []model.Author{
				{Name: "Alice Smith", SortName: "Smith, Alice"},
				{Name: "Bob Jones", SortName: "Jones, Bob"},
			}
			book, err := WriteBib(path, model.Edits{Authors: &authors})
			if err != nil {
				t.Fatal(err)
			}
			if len(book.Authors) != 2 {
				t.Fatalf("got %d authors, want 2: %+v", len(book.Authors), book.Authors)
			}
			if book.Authors[0].Name != "Alice Smith" || book.Authors[0].SortName != "Smith, Alice" {
				t.Errorf("author[0] = %+v", book.Authors[0])
			}
			if book.Authors[1].Name != "Bob Jones" || book.Authors[1].SortName != "Jones, Bob" {
				t.Errorf("author[1] = %+v", book.Authors[1])
			}
		})
	}
}

func TestWriteBibSeriesRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  string
	}{{"epub3", opf3}, {"epub2", opf2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEpub(t, baseEntries(tc.opf))
			// A fractional index (e.g. a 1.5 novella) must round-trip, not truncate.
			book, err := WriteBib(path, model.Edits{Series: ptr("The Saga"), SeriesIndex: ptr(1.5)})
			if err != nil {
				t.Fatal(err)
			}
			if book.Series != "The Saga" {
				t.Errorf("series = %q, want The Saga", book.Series)
			}
			if book.SeriesIndex != 1.5 {
				t.Errorf("series index = %v, want 1.5", book.SeriesIndex)
			}

			// Clearing it removes the series.
			book, err = WriteBib(path, model.Edits{Series: ptr("")})
			if err != nil {
				t.Fatal(err)
			}
			if book.Series != "" {
				t.Errorf("series after clear = %q, want empty", book.Series)
			}
		})
	}
}

func TestWriteBibSetsSortTitle(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	book, err := WriteBib(path, model.Edits{
		Title:     ptr("New Title"),
		SortTitle: ptr("New Title, A"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "New Title" {
		t.Errorf("title = %q, want New Title", book.Title)
	}
	if book.SortTitle != "New Title, A" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "New Title, A")
	}

	// Stored as the standard EPUB 3 file-as refine, never calibre:title_sort.
	opf, ok, _ := readEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if !bytes.Contains(opf, []byte(`property="file-as"`)) {
		t.Error("expected a file-as refine for the title sort")
	}
	if bytes.Contains(opf, []byte("calibre:title_sort")) {
		t.Error("OPF must not contain calibre:title_sort")
	}
}

func TestWriteBibSortTitleAloneLeavesTitle(t *testing.T) {
	// Setting only the sort title must not disturb the title.
	path := writeEpub(t, baseEntries(opf3))
	book, err := WriteBib(path, model.Edits{SortTitle: ptr("Sorted, Just")})
	if err != nil {
		t.Fatal(err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title (unchanged)", book.Title)
	}
	if book.SortTitle != "Sorted, Just" {
		t.Errorf("sort title = %q, want %q", book.SortTitle, "Sorted, Just")
	}
}

func TestWriteBibTitleChangeClearsStaleSortTitle(t *testing.T) {
	// opf3 starts with sort title "Title, Original"; changing the title without a
	// new sort title must clear it rather than leave a value derived from the old
	// title.
	path := writeEpub(t, baseEntries(opf3))
	before, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if before.SortTitle != "Title, Original" {
		t.Fatalf("precondition: sort title = %q, want Title, Original", before.SortTitle)
	}

	book, err := WriteBib(path, model.Edits{Title: ptr("Wuthering Heights")})
	if err != nil {
		t.Fatal(err)
	}
	if book.SortTitle != "" {
		t.Errorf("sort title = %q, want empty after a title change", book.SortTitle)
	}
}

func TestWriteBibSortTitleIgnoredForEpub2(t *testing.T) {
	// Sort titles are an EPUB 3 feature; setting one on an EPUB 2 file is a no-op
	// and must not introduce a file-as refine or a calibre:title_sort meta.
	path := writeEpub(t, baseEntries(opf2))
	book, err := WriteBib(path, model.Edits{SortTitle: ptr("Ignored, This")})
	if err != nil {
		t.Fatal(err)
	}
	if book.SortTitle != "" {
		t.Errorf("sort title = %q, want empty (ignored for EPUB 2)", book.SortTitle)
	}

	opf, ok, _ := readEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if bytes.Contains(opf, []byte(`property="file-as"`)) {
		t.Error("EPUB 2 OPF must not gain a file-as refine")
	}
	if bytes.Contains(opf, []byte("calibre:title_sort")) {
		t.Error("EPUB 2 OPF must not gain calibre:title_sort")
	}
}

func TestWriteBibAcceptsValidLanguageVerbatim(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	// A recognised tag is accepted and written through verbatim — validated, not
	// canonicalised (calibre would rewrite "pt-BR" to a 3-letter code).
	book, err := WriteBib(path, model.Edits{Language: ptr("pt-BR")})
	if err != nil {
		t.Fatal(err)
	}
	if book.Language != "pt-BR" {
		t.Errorf("language = %q, want pt-BR (verbatim, not normalised)", book.Language)
	}
}

func TestWriteBibBlankTitleRejected(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := WriteBib(path, model.Edits{Title: ptr("   ")}); err == nil {
		t.Fatal("expected error blanking title, got nil")
	}
	// Original must be untouched and still valid.
	book, err := Parse(path)
	if err != nil {
		t.Fatalf("original epub broken after rejected edit: %v", err)
	}
	if book.Title != "Original Title" {
		t.Errorf("title = %q, want Original Title (unchanged)", book.Title)
	}
}

func TestWriteBibRefusesEncryptedOPF(t *testing.T) {
	enc := `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes256-cbc"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/content.opf"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
	path := writeEpub(t, baseEntries(opf3, entry{name: "META-INF/encryption.xml", data: []byte(enc)}))
	if _, err := WriteBib(path, model.Edits{Title: ptr("Hack")}); err == nil {
		t.Fatal("expected refusal on encrypted OPF, got nil")
	}
}

func TestWriteBibAllowsFontObfuscation(t *testing.T) {
	// Font obfuscation looks like encryption but must not block metadata edits.
	enc := `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.idpf.org/2008/embedding"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/fonts/x.otf"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
	entries := baseEntries(opf3,
		entry{name: "META-INF/encryption.xml", data: []byte(enc)},
		entry{name: "OEBPS/fonts/x.otf", data: []byte("obfuscated-font")},
	)
	path := writeEpub(t, entries)
	book, err := WriteBib(path, model.Edits{Title: ptr("Obfuscated OK")})
	if err != nil {
		t.Fatalf("font obfuscation should not block edits: %v", err)
	}
	if book.Title != "Obfuscated OK" {
		t.Errorf("title = %q", book.Title)
	}
}

// --- WriteCover ------------------------------------------------------------

func TestWriteCoverReplaces(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	newCover := tinyJPEG(t)
	if _, err := WriteCover(path, "OEBPS/cover.jpg", newCover); err != nil {
		t.Fatal(err)
	}
	got, err := ExtractCover(path, "OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, newCover) {
		t.Errorf("cover = %q, want the supplied JPEG bytes", got)
	}
	// A cover swap leaves the chapter untouched.
	ch, ok, _ := readEntryFromFile(t, path, "OEBPS/chapter1.xhtml")
	if !ok || !bytes.Equal(ch, chapterBytes) {
		t.Errorf("chapter changed by cover swap")
	}
}

func TestWriteCoverRefusesEncrypted(t *testing.T) {
	enc := `<encryption xmlns="urn:oasis:names:tc:opendocument:xmlns:container" xmlns:enc="http://www.w3.org/2001/04/xmlenc#">
  <enc:EncryptedData>
    <enc:EncryptionMethod Algorithm="http://www.w3.org/2001/04/xmlenc#aes256-cbc"/>
    <enc:CipherData><enc:CipherReference URI="OEBPS/cover.jpg"/></enc:CipherData>
  </enc:EncryptedData>
</encryption>`
	path := writeEpub(t, baseEntries(opf3, entry{name: "META-INF/encryption.xml", data: []byte(enc)}))
	if _, err := WriteCover(path, "OEBPS/cover.jpg", tinyJPEG(t)); err == nil {
		t.Fatal("expected refusal on encrypted cover, got nil")
	}
}

func TestWriteCoverRefusesNonRaster(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := WriteCover(path, "OEBPS/cover.svg", []byte("<svg/>")); err == nil {
		t.Fatal("expected refusal on non-raster cover format, got nil")
	}
}

func TestWriteCoverRejectsNonImage(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := WriteCover(path, "OEBPS/cover.jpg", []byte("definitely not an image")); err == nil {
		t.Fatal("expected rejection of non-image cover data, got nil")
	}
}

func TestWriteCoverRejectsFormatMismatch(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	// PNG bytes into a .jpg cover entry must be rejected — we do not transcode.
	if _, err := WriteCover(path, "OEBPS/cover.jpg", tinyPNG(t)); err == nil {
		t.Fatal("expected rejection of PNG bytes into a .jpg cover entry, got nil")
	}
}
