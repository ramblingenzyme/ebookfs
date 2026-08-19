package epub

import (
	"archive/zip"
	"bytes"
	"image"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

// --- writer-only helpers ---------------------------------------------------

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
			book, err := writeBib(path, model.Edits{
				Title:       new("New Title"),
				Description: new("New description."),
				Language:    new("fr"),
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
	if _, err := writeBib(path, model.Edits{Title: new("Another Title")}); err != nil {
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
			book, err := writeBib(path, model.Edits{Authors: &authors})
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
			book, err := writeBib(path, model.Edits{Series: new("The Saga"), SeriesIndex: new("1.5")})
			if err != nil {
				t.Fatal(err)
			}
			if book.Series == nil || book.Series.Name != "The Saga" {
				t.Errorf("series = %v, want The Saga", book.Series)
			}
			if book.Series == nil || book.Series.Index != "1.5" {
				t.Errorf("series index = %v, want 1.5", book.Series.Index)
			}

			// Clearing it removes the series.
			book, err = writeBib(path, model.Edits{Series: new(string)})
			if err != nil {
				t.Fatal(err)
			}
			if book.Series != nil {
				t.Errorf("series after clear = %+v, want nil", book.Series)
			}
		})
	}
}

func TestWriteBibSetsSortTitle(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	book, err := writeBib(path, model.Edits{
		Title:     new("New Title"),
		SortTitle: new("New Title, A"),
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
	book, err := writeBib(path, model.Edits{SortTitle: new("Sorted, Just")})
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

	book, err := writeBib(path, model.Edits{Title: new("Wuthering Heights")})
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
	book, err := writeBib(path, model.Edits{SortTitle: new("Ignored, This")})
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
	book, err := writeBib(path, model.Edits{Language: new("pt-BR")})
	if err != nil {
		t.Fatal(err)
	}
	if book.Language != "pt-BR" {
		t.Errorf("language = %q, want pt-BR (verbatim, not normalised)", book.Language)
	}
}

func TestWriteBibBlankTitleRejected(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := writeBib(path, model.Edits{Title: new("   ")}); err == nil {
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
	if _, err := writeBib(path, model.Edits{Title: new("Hack")}); err == nil {
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
	book, err := writeBib(path, model.Edits{Title: new("Obfuscated OK")})
	if err != nil {
		t.Fatalf("font obfuscation should not block edits: %v", err)
	}
	if book.Title != "Obfuscated OK" {
		t.Errorf("title = %q", book.Title)
	}
}

func TestWriteBibWithDirectoryEntries(t *testing.T) {
	entries := baseEntries(opf3,
		entry{name: "OEBPS/", data: nil},
		entry{name: "fonts/", data: nil},
		entry{name: "images/", data: nil},
		entry{name: "text/", data: nil},
	)
	path := writeEpub(t, entries)
	book, err := writeBib(path, model.Edits{Title: new("New Title")})
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

func TestWriteCoverWithDirectoryEntries(t *testing.T) {
	entries := baseEntries(opf3,
		entry{name: "OEBPS/", data: nil},
		entry{name: "fonts/", data: nil},
	)
	path := writeEpub(t, entries)
	newCover := tinyJPEG(t)
	if _, err := writeCover(path, "OEBPS/cover.jpg", newCover); err != nil {
		t.Fatal(err)
	}
	r, err := OpenReader(path, "OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Cover()
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

// --- WriteCover ------------------------------------------------------------

func TestWriteCoverReplaces(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	newCover := tinyJPEG(t)
	if _, err := writeCover(path, "OEBPS/cover.jpg", newCover); err != nil {
		t.Fatal(err)
	}
	r, err := OpenReader(path, "OEBPS/cover.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Cover()
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
	if _, err := writeCover(path, "OEBPS/cover.jpg", tinyJPEG(t)); err == nil {
		t.Fatal("expected refusal on encrypted cover, got nil")
	}
}

func TestWriteCoverRefusesNonRaster(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := writeCover(path, "OEBPS/cover.svg", []byte("<svg/>")); err == nil {
		t.Fatal("expected refusal on non-raster cover format, got nil")
	}
}

func TestWriteCoverRejectsNonImage(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	if _, err := writeCover(path, "OEBPS/cover.jpg", []byte("definitely not an image")); err == nil {
		t.Fatal("expected rejection of non-image cover data, got nil")
	}
}

func TestWriteCoverRejectsFormatMismatch(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	// PNG bytes into a .jpg cover entry must be rejected — we do not transcode.
	if _, err := writeCover(path, "OEBPS/cover.jpg", tinyPNG(t)); err == nil {
		t.Fatal("expected rejection of PNG bytes into a .jpg cover entry, got nil")
	}
}

// reindexSeries applies an index-only series edit to the epub at path, with the
// book model claiming series. Validate refuses a SeriesIndex edit on a book
// with no series at all, so the model has to carry one — which is exactly the
// shape library.Edit hands in, having read the book from the index.
func reindexSeries(t *testing.T, path, series, index string) model.Bib {
	t.Helper()
	b := &model.Book{
		Location: model.Location{EpubPath: path},
		Bib:      model.Bib{Series: &model.SeriesRef{Name: series, Index: "1"}},
	}
	bib, err := Rewrite(path, b, model.Edits{SeriesIndex: new(index)})
	if err != nil {
		t.Fatalf("Rewrite: %v", err)
	}

	return bib
}

// TestWriteBibSeriesIndexOnlyKeepsName covers the index-only series edit. With
// no Series in the edits there is no name to write, so the only source is the
// OPF itself — which is what currentSeriesName is for. Get it wrong and moving
// a book to a new position in its series silently drops the series.
func TestWriteBibSeriesIndexOnlyKeepsName(t *testing.T) {
	for _, tc := range []struct {
		name string
		opf  string
	}{{"epub3", opf3}, {"epub2", opf2}} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeEpub(t, baseEntries(tc.opf))
			if _, err := writeBib(path, model.Edits{Series: new("The Saga"), SeriesIndex: new("1")}); err != nil {
				t.Fatal(err)
			}

			book := reindexSeries(t, path, "The Saga", "4")

			if book.Series == nil || book.Series.Name != "The Saga" {
				t.Errorf("series = %v after an index-only edit, want it carried over from the OPF", book.Series)
			}
			if book.Series == nil || book.Series.Index != "4" {
				t.Errorf("series index = %v, want 4", book.Series.Index)
			}
		})
	}
}

// TestWriteBibSeriesIndexOnlyWithoutSeriesInOPF pins what happens when the two
// sources disagree: the index says the book is in a series, the epub has no
// such metadata. There is no name to write the new position against, so the
// edit is dropped rather than inventing an empty collection.
func TestWriteBibSeriesIndexOnlyWithoutSeriesInOPF(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3)) // no series metadata

	book := reindexSeries(t, path, "Phantom Saga", "4")

	if book.Series != nil {
		t.Errorf("series = %+v, want nil — the OPF has none to carry over, and the edit must not invent one", book.Series)
	}
	// SeriesIndex is deliberately not asserted: translateSeries defaults it to
	// 1 for every book with a series, and only sets Series when the name is
	// non-empty, so the position never escapes.
}

// --- Metadata we must not clobber -----------------------------------------

func TestSetSeriesPreservesExistingIndex(t *testing.T) {
	path := writeEpub(t, baseEntries(opf3))
	// First, set up a series with index 3
	if _, err := writeBib(path, model.Edits{Series: new("The Trilogy"), SeriesIndex: new("3")}); err != nil {
		t.Fatal(err)
	}

	// Now rename the series without setting index
	book, err := writeBib(path, model.Edits{Series: new("The Quartet")})
	if err != nil {
		t.Fatal(err)
	}

	// The index should be preserved as 3, not reset to 1
	if book.Series == nil || book.Series.Index != "3" {
		t.Errorf("series index = %v, want 3.0 (preserved from before rename)", book.Series.Index)
	}
}

// opfWithAlternateScript carries a refinement ebookfs does not manage
// (alternate-script, a publisher/Calibre convention) alongside the role and
// file-as it does.
var opfWithAlternateScript = opf3With(`    <meta refines="#creator1" property="alternate-script" xml:lang="ja">ドゥ・ジェーン</meta>`)

// opfSeriesWithIdentifier refines the collection with a dcterms:identifier —
// EPUB 3 lets a series carry an ISSN — which is not ours to rewrite.
var opfSeriesWithIdentifier = opf3With(`    <meta property="belongs-to-collection" id="series1">The Trilogy</meta>
    <meta refines="#series1" property="collection-type">series</meta>
    <meta refines="#series1" property="group-position">2</meta>
    <meta refines="#series1" property="dcterms:identifier">urn:issn:1234-5678</meta>`)

// TestSetSeriesReusesCollection pins that a series edit rewrites the collection
// element it found rather than replacing it, so a refinement ebookfs does not
// manage survives — and that clearing the series still takes the whole thing.
func TestSetSeriesReusesCollection(t *testing.T) {
	path := writeEpub(t, baseEntries(opfSeriesWithIdentifier))

	book, err := writeBib(path, model.Edits{Series: new("The Quartet")})
	if err != nil {
		t.Fatal(err)
	}
	if book.Series == nil || book.Series.Name != "The Quartet" || book.Series.Index != "2" {
		t.Fatalf("series = %+v, want The Quartet at 2", book.Series)
	}

	opfBytes, ok, _ := readEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if !bytes.Contains(opfBytes, []byte("urn:issn:1234-5678")) {
		t.Error("the collection identifier was dropped by a rename")
	}
	// The element is reused, so it keeps its own id rather than being minted a
	// fresh one, and the refines above still point at it.
	if !bytes.Contains(opfBytes, []byte(`id="series1"`)) {
		t.Error("collection id changed; refinements no longer target it")
	}
	for _, p := range []string{"collection-type", "group-position"} {
		want := []byte(`refines="#series1" property="` + p + `"`)
		if n := bytes.Count(opfBytes, want); n != 1 {
			t.Errorf("%s refines = %d, want 1 (rewritten, not duplicated)", p, n)
		}
	}

	// Clearing the series takes the element with it, identifier and all.
	if _, err := writeBib(path, model.Edits{Series: new(string)}); err != nil {
		t.Fatal(err)
	}
	opfBytes, ok, _ = readEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}
	if bytes.Contains(opfBytes, []byte("series1")) {
		t.Error("clearing the series left the collection behind")
	}
}

func TestSetSeriesPreservesSets(t *testing.T) {
	opfWithSet := opf3With(`    <!-- Series -->
    <meta property="belongs-to-collection" id="series1">The Trilogy</meta>
    <meta refines="#series1" property="collection-type">series</meta>
    <meta refines="#series1" property="group-position">2</meta>

    <!-- Set (bundle) -->
    <meta property="belongs-to-collection" id="set1">Complete Works</meta>
    <meta refines="#set1" property="collection-type">set</meta>`)

	path := writeEpub(t, baseEntries(opfWithSet))

	// Edit the series
	book, err := writeBib(path, model.Edits{Series: new("The Quartet"), SeriesIndex: new("1")})
	if err != nil {
		t.Fatal(err)
	}

	// The series should be updated
	if book.Series == nil || book.Series.Name != "The Quartet" {
		t.Errorf("series = %v, want The Quartet", book.Series)
	}

	// The set should still be present in the OPF
	opfBytes, ok, _ := readEntryFromFile(t, path, "OEBPS/content.opf")
	if !ok {
		t.Fatal("OPF entry not found")
	}

	if !bytes.Contains(opfBytes, []byte("Complete Works")) {
		t.Error("set (Complete Works) was removed, should be preserved")
	}

	if !bytes.Contains(opfBytes, []byte(`collection-type">set`)) {
		t.Error("set collection-type was removed, should be preserved")
	}
}

// TestSetAuthorsReuseBookkeeping covers what reusing a creator element has to
// get right: a refinement ebookfs does not manage survives an edit, an author
// dropped from the list takes its refinements with it (nothing may keep
// pointing at a creator that is gone), the written order is the order given
// rather than the order the OPF happened to hold, and the managed refinements
// are rewritten rather than duplicated. Each step edits the file the previous
// one left behind.
func TestSetAuthorsReuseBookkeeping(t *testing.T) {
	path := writeEpub(t, baseEntries(opfWithAlternateScript))

	opf := func(t *testing.T) []byte {
		t.Helper()
		b, ok, _ := readEntryFromFile(t, path, "OEBPS/content.opf")
		if !ok {
			t.Fatal("OPF entry not found")
		}
		return b
	}

	t.Run("rewrite", func(t *testing.T) {
		// The author list is unchanged, so the creator is reused as-is.
		authors := []model.Author{{Name: "Jane Doe", SortName: "Doe, Jane"}}
		book, err := writeBib(path, model.Edits{Authors: &authors})
		if err != nil {
			t.Fatal(err)
		}
		if len(book.Authors) != 1 || book.Authors[0].Name != "Jane Doe" || book.Authors[0].SortName != "Doe, Jane" {
			t.Errorf("authors = %+v, want Jane Doe / Doe, Jane", book.Authors)
		}
		if n := bytes.Count(opf(t), []byte(`property="alternate-script"`)); n != 1 {
			t.Errorf("alternate-script count = %d, want 1", n)
		}
	})

	t.Run("reorder and clear sort name", func(t *testing.T) {
		authors := []model.Author{{Name: "Bob Jones", SortName: "Jones, Bob"}, {Name: "Jane Doe"}}
		book, err := writeBib(path, model.Edits{Authors: &authors})
		if err != nil {
			t.Fatal(err)
		}
		if len(book.Authors) != 2 || book.Authors[0].Name != "Bob Jones" || book.Authors[1].Name != "Jane Doe" {
			t.Fatalf("authors = %+v, want Bob Jones then Jane Doe", book.Authors)
		}
		// Jane kept her element, so her sort name has to be cleared rather than
		// left over from the previous write.
		if book.Authors[1].SortName != "" {
			t.Errorf("Jane sort name = %q, want empty", book.Authors[1].SortName)
		}

		b := opf(t)
		if n := bytes.Count(b, []byte(`property="alternate-script"`)); n != 1 {
			t.Errorf("alternate-script count = %d, want 1", n)
		}
		if n := bytes.Count(b, []byte(`refines="#creator1" property="role"`)); n != 1 {
			t.Errorf("role refines on creator1 = %d, want 1 (rewritten, not duplicated)", n)
		}
		if bytes.Contains(b, []byte(`refines="#creator1" property="file-as"`)) {
			t.Error("creator1 kept a file-as refine after its sort name was cleared")
		}
	})

	t.Run("drop", func(t *testing.T) {
		// Jane's element goes, and so must everything refining it.
		authors := []model.Author{{Name: "Bob Jones", SortName: "Jones, Bob"}}
		if _, err := writeBib(path, model.Edits{Authors: &authors}); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(opf(t), []byte("creator1")) {
			t.Error("refinements still point at creator1 after the author was dropped")
		}
	})
}
