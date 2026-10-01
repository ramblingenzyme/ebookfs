package library_test

import (
	"archive/zip"
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

// The duplicate rule compares author sets, so either order is one book.
// Regression: a path lookup filed "A & B" and "B & A" as two books.
func TestIngestDuplicateIgnoresAuthorOrder(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Good Omens", "Neil Gaiman", "Terry Pratchett"))

	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(buildTestEpub(t, "Good Omens", "Terry Pratchett", "Neil Gaiman"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := h.Ingest(); !errors.Is(err, library.ErrDuplicate) {
		t.Fatalf("second ingest err = %v, want ErrDuplicate", err)
	}
}

// The title query matches it; only the author set tells the two apart.
func TestIngestSameTitleDifferentAuthors(t *testing.T) {
	lib := openTestLibrary(t)

	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Alice"))
	ingestTestEpub(t, lib, buildTestEpub(t, "Selected Poems", "Bob"))
}

// Not an epub is the one ingest failure the uploader can fix, so it has a
// sentinel.
func TestIngestRejectsAFileThatIsNotAnEpub(t *testing.T) {
	lib := openTestLibrary(t)

	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt([]byte("this is plainly not a zip archive"), 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	if _, err := h.Ingest(); !errors.Is(err, library.ErrNotEpub) {
		t.Fatalf("ingest err = %v, want ErrNotEpub", err)
	}
}

// TestIngestPreservesAllMetadataFields verifies that all metadata fields
// survive the parse → index → query flow. This catches bugs where a field
// is forgotten during translation between layers.
func TestIngestPreservesAllMetadataFields(t *testing.T) {
	lib := openTestLibrary(t)

	// Build an EPUB with all metadata fields populated
	epubData := buildFullMetadataEpub(t)
	book := ingestTestEpub(t, lib, epubData)

	// Retrieve the book and verify all fields
	retrieved, err := lib.Get(book.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Basic fields
	if retrieved.Title() != "Complete Metadata Test" {
		t.Errorf("Title = %q, want %q", retrieved.Title(), "Complete Metadata Test")
	}
	if retrieved.Language() != "en" {
		t.Errorf("Language = %q, want %q", retrieved.Language(), "en")
	}
	if retrieved.Description() != "A test book with all metadata fields" {
		t.Errorf("Description = %q, want %q", retrieved.Description(), "A test book with all metadata fields")
	}

	// Authors
	authors := retrieved.Authors()
	if len(authors) != 2 {
		t.Fatalf("len(Authors) = %d, want 2", len(authors))
	}
	if authors[0].Name != "First Author" {
		t.Errorf("Authors[0].Name = %q, want %q", authors[0].Name, "First Author")
	}
	if authors[1].Name != "Second Author" {
		t.Errorf("Authors[1].Name = %q, want %q", authors[1].Name, "Second Author")
	}

	// Publisher
	if retrieved.Publisher() != "Test Publisher" {
		t.Errorf("Publisher = %q, want %q", retrieved.Publisher(), "Test Publisher")
	}

	// Rights
	if retrieved.Rights() != "Copyright 2024 Test" {
		t.Errorf("Rights = %q, want %q", retrieved.Rights(), "Copyright 2024 Test")
	}

	// Subjects
	subjects := retrieved.Subjects()
	wantSubjects := []string{"Fiction", "Testing"}
	if !slices.Equal(subjects, wantSubjects) {
		t.Errorf("Subjects = %v, want %v", subjects, wantSubjects)
	}

	// Contributors
	contributors := retrieved.Contributors()
	if len(contributors) != 2 {
		t.Fatalf("len(Contributors) = %d, want 2", len(contributors))
	}
	if contributors[0].Name != "Test Editor" || contributors[0].Role != "edt" {
		t.Errorf("Contributors[0] = %+v, want {Name: Test Editor, Role: edt}", contributors[0])
	}
	if contributors[1].Name != "Test Translator" || contributors[1].Role != "trl" {
		t.Errorf("Contributors[1] = %+v, want {Name: Test Translator, Role: trl}", contributors[1])
	}

	// Identifiers
	ids := retrieved.Identifiers()
	if ids["isbn"] != "978-3-16-148410-0" {
		t.Errorf("Identifiers[isbn] = %q, want %q", ids["isbn"], "978-3-16-148410-0")
	}
	if ids["id"] != "test-uuid-12345" {
		t.Errorf("Identifiers[id] = %q, want %q", ids["id"], "test-uuid-12345")
	}
}

// buildFullMetadataEpub creates an EPUB with all metadata fields populated
func buildFullMetadataEpub(t *testing.T) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)

	// mimetype must be first and uncompressed
	mt, err := zw.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	mt.Write([]byte("application/epub+zip"))

	files := map[string]string{
		"META-INF/container.xml": `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>`,
		"content.opf": `<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="id">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:identifier id="id">test-uuid-12345</dc:identifier>
    <dc:identifier id="isbn">978-3-16-148410-0</dc:identifier>
    <dc:title>Complete Metadata Test</dc:title>
    <dc:creator id="author1">First Author</dc:creator>
    <dc:creator id="author2">Second Author</dc:creator>
    <dc:contributor id="editor">Test Editor</dc:contributor>
    <meta refines="#editor" property="role" scheme="marc:relators">edt</meta>
    <dc:contributor id="translator">Test Translator</dc:contributor>
    <meta refines="#translator" property="role" scheme="marc:relators">trl</meta>
    <dc:language>en</dc:language>
    <dc:publisher>Test Publisher</dc:publisher>
    <dc:rights>Copyright 2024 Test</dc:rights>
    <dc:description>A test book with all metadata fields</dc:description>
    <dc:subject>Fiction</dc:subject>
    <dc:subject>Testing</dc:subject>
  </metadata>
  <manifest>
    <item id="cover" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
  </manifest>
</package>`,
		"cover.jpg": "placeholder-cover-bytes",
	}

	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(content))
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
