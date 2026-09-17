// Organised by metadata vocabulary rather than by function: EPUB 3.3 Appendix D
// and OPF 2.0 publication metadata. A TestSpec* name means the assertion cites a
// section, so a failure means the package stopped conforming. Several add a
// write-side assertion no spec requires and say so.
//
// Fixtures are minimal, not valid. No dcterms:modified, no nav document, and
// epub2() names an ncx its manifest lacks. Each conforms only in the respect its
// own test is about.
//
// https://www.w3.org/TR/epub-33/#app-meta-property-vocab
// https://idpf.org/epub/20/spec/OPF_2.0_final_spec.html
package epub_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	bookmodel "github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub"
	"github.com/ramblingenzyme/ebookfs/library/internal/epub/edits"
)

// --- dc:title selection ---
//
// EPUB 3.3 §5.5.3.1.2: "The first dc:title element in document order is the main
// title of the EPUB publication." OPF 2.0 §2.2.1 defines no algorithm and
// endorses "either the first title element or all the title elements".

var opfTitleTypes = epub3(`    <dc:title id="t1">The Complete Trilogy</dc:title>
    <meta refines="#t1" property="title-type">collection</meta>
    <dc:title id="t2">The Fellowship</dc:title>
    <meta refines="#t2" property="title-type">main</meta>
    <meta refines="#t2" property="file-as">Fellowship, The</meta>
    <dc:title id="t3">Being the First Part</dc:title>
    <meta refines="#t3" property="title-type">subtitle</meta>`)

// §5.5.3.1.2: the first dc:title in document order wins, read and written, even
// when a later one is labelled "main". calibre picks that later one; deliberate
// divergence. Replacing the title leaves one dc:title, so no file survives on
// which the two readings disagree.
func TestSpecFirstTitleWins(t *testing.T) {
	path := buildEpub(t, opfTitleTypes)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "The Complete Trilogy" {
		t.Errorf("title = %q, want the first dc:title per §5.5.3.1.2", bib.Title)
	}

	// The write side must target the same element the read side resolved, or the
	// edit would appear not to happen.
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)
	if got := titleByID(md, "t1"); got != want {
		t.Errorf("first title = %q, want %q — read and write must resolve the same element", got, want)
	}

	// Left behind, t2 is what calibre would still show as this book's title.
	if els := md.SelectElements("title"); len(els) != 1 {
		t.Errorf("dc:title count = %d, want the one the edit left", len(els))
	}
	for _, id := range []string{"t2", "t3"} {
		if got := titleByID(md, id); got != "" {
			t.Errorf("title %s = %q, want it dropped with the title it was a segment of", id, got)
		}
		for _, m := range md.SelectElements("meta") {
			if m.SelectAttrValue("refines", "") == "#"+id {
				t.Errorf("refinement of the dropped title %s survived: %s", id, m.Text())
			}
		}
	}
}

// --- display-seq ---
//
// D.3.5: display-seq "only applies where precedence rules have not already been
// defined (e.g., precedence is given to creators based on their appearance in
// document order)". §5.5.3.2.3 defines exactly that for creators, and OPF 2.0
// §2.2.2 agrees. So display-seq is inert on creators in both specs.

var opfDisplaySeq = epub3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c1" property="display-seq">2</meta>
    <dc:creator id="c2">Bo Li</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c2" property="display-seq">1</meta>`)

// D.3.5: display-seq does not apply once an order is defined, and for creators
// document order is one. A contradicting display-seq must not reorder authors.
func TestSpecCreatorOrderIsDocumentOrder(t *testing.T) {
	path := buildEpub(t, opfDisplaySeq)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want document order [Ann Rand, Bo Li] per §5.5.3.2.3", got)
	}
}

// --- dcterms:modified ---

// §5.5.5 requires exactly one dcterms:modified, UTC, Z-terminated, in the
// extended format. Updating it on a change is only a lowercase "should"
// (§1.5), asserted here anyway.
func TestSpecModifiedIsUpdated(t *testing.T) {

	path := buildEpub(t, richOPF3) // carries dcterms:modified 2020-01-02T00:00:00Z
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}
	got := metadata(t, path).FindElement("//meta[@property='dcterms:modified']")
	if got == nil {
		t.Fatal("dcterms:modified was removed")
	}
	if got.Text() == "2020-01-02T00:00:00Z" {
		t.Errorf("dcterms:modified = %q, want the time of this rewrite", got.Text())
	}
	// Format and cardinality are MUST, unlike the update itself.
	if n := len(metadata(t, path).FindElements("//meta[@property='dcterms:modified']")); n != 1 {
		t.Errorf("dcterms:modified count = %d, want exactly one per §5.5.5", n)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", got.Text()); err != nil {
		t.Errorf("dcterms:modified = %q, want the extended UTC format YYYY-MM-DDThh:mm:ssZ", got.Text())
	}
}

// --- collection-type ---
//
// D.3.4 defines exactly two collection-type values when no scheme is given:
// series and set. "publisher-series" below is deliberately neither: the
// unrecognised case, which must not be taken for the book's series.

var opfCollections = epub3(metas(
	collection("ps1", "Acme Classics", "publisher-series", ""),
	collection("set1", "Complete Works", "set", ""),
	collection("s1", "The Trilogy", "series", "2", "set1"),
))

// D.3.4: only an unschemed series collection is the book's series, so neither a
// publisher-series nor a set counts. D.3.3 covers the nesting. Renaming in
// place and leaving the parent alone is a choice; no spec says what a rename does.
func TestSpecOnlySeriesCollectionIsTheSeries(t *testing.T) {
	path := buildEpub(t, opfCollections)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Trilogy" || bib.Series.Index != "2" {
		t.Fatalf("series = %+v, want The Trilogy at 2", bib.Series)
	}

	want := "The Quartet"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Series: &want}); err != nil {
		t.Fatal(err)
	}
	md := metadata(t, path)

	for _, c := range []struct{ id, text string }{
		{"ps1", "Acme Classics"},
		{"set1", "Complete Works"},
	} {
		if got := property(t, md, "//meta[@id='"+c.id+"']"); got != c.text {
			t.Errorf("collection %s = %q, want %q untouched", c.id, got, c.text)
		}
	}
	series := md.FindElement("//meta[@id='s1']")
	if series == nil {
		t.Fatal("the series collection was replaced rather than rewritten")
	}
	if series.Text() != want {
		t.Errorf("series = %q, want %q", series.Text(), want)
	}
	if got := series.SelectAttrValue("refines", ""); got != "#set1" {
		t.Errorf("series nesting = %q, want it still inside #set1", got)
	}
}

// --- role ---
//
// EPUB 3 carries the MARC relator as a role refine, EPUB 2 as opf:role. Neither
// spec states that an omitted role means "author": that is our reading of
// dc:creator as the party "responsible for the creation of the content"
// (3.3 §5.5.3.2.3) / "A primary creator or author of the publication"
// (OPF 2.0 §2.2.2). Pinned here as an interpretation, not a conformance claim.

func TestSpecOnlyAuthorRoleCreatorsAreAuthors(t *testing.T) {
	var opf = epub3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <dc:creator id="c2">Acme Editorial Board</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">edt</meta>
    <dc:creator id="c3">Bo Li</dc:creator>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	// c2 is an editor, so it is not an author; c3 has no role, which defaults
	// to author.
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want [Ann Rand, Bo Li]", got)
	}

	// An authors edit must not disturb the editor, who is not ours to rewrite.
	authors := []bookmodel.Author{{Name: "Ann Rand"}}
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}
	if got := textOf(t, metadata(t, path), "//creator[@id='c2']"); got != "Acme Editorial Board" {
		t.Error("the editor was removed by an authors edit")
	}
}

// --- multiple dc:language ---

// §5.5.3.1.3: the first dc:language in document order is primary. OPF 2.0
// §2.2.12 allows several and names no rule, so first-wins is a choice for v2.
func TestSpecFirstLanguageWins(t *testing.T) {
	var opf = epub3(`    <dc:language>en</dc:language>
    <dc:language>fr</dc:language>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Language != "en" {
		t.Errorf("language = %q, want the first of en, fr", bib.Language)
	}

	// Editing it rewrites the first and leaves the second alone.
	fr := "de"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Language: &fr}); err != nil {
		t.Fatal(err)
	}
	langs := metadata(t, path).SelectElements("language")
	if len(langs) != 2 {
		t.Fatalf("dc:language count = %d, want 2", len(langs))
	}
	if langs[0].Text() != "de" || langs[1].Text() != "fr" {
		t.Errorf("languages = %q, %q, want de, fr", langs[0].Text(), langs[1].Text())
	}
}

// §5.5.2 requires non-empty values, so a nameless belongs-to-collection is
// invalid and the read falls through to the calibre metas. A writer using its
// own rule reads "no series" and an index-only edit deletes one that was showing.
func TestSpecSeriesCarryOverMatchesWhatTheReaderSees(t *testing.T) {
	path := buildEpub(t, epub3(metas(
		collection("c01", "", "series", ""),
		calibreSeries("The Trilogy", "3"),
	)))

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Trilogy" {
		t.Fatalf("series before the edit = %+v, want The Trilogy from the calibre metas", bib.Series)
	}

	index := "5"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{SeriesIndex: &index}); err != nil {
		t.Fatal(err)
	}
	bib, err = epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil {
		t.Fatal("an index-only edit deleted the series the reader reported")
	}
	if bib.Series.Name != "The Trilogy" || bib.Series.Index != index {
		t.Errorf("series = %+v, want The Trilogy at %s", bib.Series, index)
	}
}

// --- id uniqueness ---
//
// XML 1.0 §3.3.1: "ID values MUST uniquely identify the elements which bear
// them." Not cosmetic. §5.3.6 makes refines target by fragment, so a duplicate
// id binds a refinement to both elements and epubcheck rejects the package.

func TestSpecMintedIDsDoNotCollide(t *testing.T) {
	// Each fixture already contains an element squatting on the id the writer
	// would otherwise mint, on a *different* kind of element than the one being
	// written, the case a per-kind scan cannot see.
	for _, tc := range []struct {
		name  string
		meta  string
		edits edits.Edits
	}{
		{
			name: "title sort vs a squatted ebookfs-title",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="ebookfs-title">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edits: edits.Edits{SortTitle: new("Title, The")},
		},
		{
			// Both spellings the minter has ever produced are squatted, on elements
			// that are not creators, precisely what the old creator-only scan missed.
			name: "new creator vs a squatted ebookfs-creator",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-creator">The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <dc:language id="ebookfs-creator-1">en</dc:language>`,
			edits: edits.Edits{Authors: &[]bookmodel.Author{{Name: "Someone Else"}}},
		},
		{
			name: "new collection vs a squatted ebookfs-series",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-series">The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edits: edits.Edits{Series: new("The Trilogy")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := buildEpub(t, epub3(tc.meta))
			if _, err := epub.Rewrite(path, book(t, path), tc.edits); err != nil {
				t.Fatal(err)
			}
			assertUniqueIDs(t, path)
		})
	}
}

// The writer mints ids against a document already holding ids it minted before,
// so growing the author list one edit at a time must not repeat a name.
func TestSpecRepeatedEditsDoNotCollideIDs(t *testing.T) {
	path := buildEpub(t, epub3(``))

	names := []string{"Ann Rand"}
	for _, add := range []string{"Bo Carr", "Cy Dunn", "Di Ekko"} {
		names = append(names, add)
		authors := make([]bookmodel.Author, len(names))
		for i, n := range names {
			authors[i] = bookmodel.Author{Name: n}
		}
		if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
			t.Fatalf("adding %s: %v", add, err)
		}
		assertUniqueIDs(t, path)

		bib, err := epub.Parse(path)
		if err != nil {
			t.Fatalf("after adding %s: %v", add, err)
		}
		if got := authorNames(bib); !slices.Equal(got, names) {
			t.Fatalf("authors = %v, want %v", got, names)
		}
	}
}

// An author placed before an existing one is minted an id while that creator is
// not yet back in the tree. Scanning only current creators cannot see the
// duplicate, and duplicate ids cross-wire refinements.
func TestSpecReorderingAuthorsKeepsIDsUnique(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:creator id="ebookfs-creator">Alice</dc:creator>
    <meta refines="#ebookfs-creator" property="file-as">Alice, A</meta>`))

	authors := []bookmodel.Author{{Name: "Bob", SortName: "Bob, B"}, {Name: "Alice", SortName: "Alice, A"}}
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}
	assertUniqueIDs(t, path)

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Bob", "Alice"}) {
		t.Fatalf("authors = %v, want [Bob Alice] in the order given", got)
	}
	for i, want := range []string{"Bob, B", "Alice, A"} {
		if got := bib.Authors[i].SortName; got != want {
			t.Errorf("%s sort name = %q, want %q", bib.Authors[i].Name, got, want)
		}
	}
}

// assertUniqueIDs checks the whole package document, not just <metadata>: a
// minted id can collide with a manifest item's just as easily.
func assertUniqueIDs(t *testing.T, path string) {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(readEntry(t, path, opfPath)); err != nil {
		t.Fatalf("result is not parseable XML: %v", err)
	}
	seen := map[string]bool{}
	for _, el := range doc.FindElements("//*[@id]") {
		id := el.SelectAttrValue("id", "")
		if seen[id] {
			t.Errorf("id %q appears on more than one element; XML 1.0 §3.3.1 requires ID values to be unique", id)
		}
		seen[id] = true
	}
}

func titleByID(md *etree.Element, id string) string {
	for _, el := range md.SelectElements("title") {
		if el.SelectAttrValue("id", "") == id {
			return el.Text()
		}
	}
	return ""
}

func authorNames(b *bookmodel.Bib) []string {
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

var opfSpecStyleWhitespace = epub3(`    <dc:identifier id="pub-id">
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

// A wrapped role refine reads as "\n      aut\n    ", so the document parses as
// "no authors". The same raw comparison drops the series and stores a SortTitle
// with leading newlines, the key for the default list order.
func TestSpecWhitespaceIsCollapsed(t *testing.T) {

	bib, err := epub.Parse(buildEpub(t, opfSpecStyleWhitespace))
	if err != nil {
		t.Fatalf("a document formatted the way the spec prints its own examples must parse: %v", err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Haruki Murakami"}) {
		t.Errorf("authors = %v, want [Haruki Murakami]", got)
	}
	if bib.SortTitle != "Norwegian Wood" {
		t.Errorf("sort title = %q, want it collapsed and trimmed", bib.SortTitle)
	}
	if bib.Series == nil || bib.Series.Name != "The New French Cuisine Masters" || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want The New French Cuisine Masters at 2", bib.Series)
	}
	if got := bib.Identifiers["uuid"]; got != "A1B0D67E" {
		t.Errorf("identifier = %q, want it collapsed and trimmed", got)
	}
}

// XML 1.0 §3.3.3 turns a newline inside an attribute value into a space without
// trimming it, so a wrapped opf:role arrives padded.
func TestSpecWhitespaceInEPUB2RoleAttribute(t *testing.T) {

	opf := epub2(`    <dc:title>Alice in Wonderland</dc:title>
    <dc:creator opf:role=" aut " opf:file-as="Carroll, Lewis">Lewis Carroll</dc:creator>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatalf("a padded opf:role must still be an author role: %v", err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll]", got)
	}
}

// The package version attribute is the only one read without attr(), so a
// wrapped version="3.0" reports EPUB 2. §5.5.5's dcterms:modified then never
// updates, and calibre metas are injected into a package that had none.
func TestSpecWhitespaceInTheVersionAttribute(t *testing.T) {
	opf := packageDoc(strings.Replace(string(epub3(`    <meta property="dcterms:modified">2020-01-02T00:00:00Z</meta>`)),
		`version="3.0"`, "version=\"\n      3.0\n    \"", 1))

	path := buildEpub(t, opf)
	sort := "Title, The"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{SortTitle: &sort}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	got := md.FindElement("//meta[@property='dcterms:modified']")
	if got == nil {
		t.Fatal("dcterms:modified was removed")
	}
	if got.Text() == "2020-01-02T00:00:00Z" {
		t.Errorf("dcterms:modified = %q, want the time of this rewrite — the padded version read as EPUB 2", got.Text())
	}
	for _, m := range md.SelectElements("meta") {
		if name := m.SelectAttrValue("name", ""); strings.HasPrefix(name, "calibre:") {
			t.Errorf("%s was injected into an EPUB 3 package", name)
		}
	}
}

// --- the prefix attribute ---
//
// D.1.4: "EPUB creators MUST declare the prefix mappings they use in the prefix
// attribute of the package element." D.1.5 reserves a set that need no
// declaration and only SHOULD NOT be overridden, so a document may rebind one.

func TestSpecDeclaredPrefixResolvesToTheSameProperty(t *testing.T) {
	opf := epub3(`    <meta property="dct:modified">2020-01-02T00:00:00Z</meta>`).
		attr(`prefix="dct: http://purl.org/dc/terms/"`)

	path := buildEpub(t, opf)
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}

	// §5.5.5: exactly one, whichever prefix spells it, and freshly written.
	var modified []*etree.Element
	for _, m := range metadata(t, path).SelectElements("meta") {
		if p := m.SelectAttrValue("property", ""); p == "dct:modified" || p == "dcterms:modified" {
			modified = append(modified, m)
		}
	}
	if len(modified) != 1 {
		t.Fatalf("last-modified properties = %d, want exactly one per §5.5.5", len(modified))
	}
	if modified[0].Text() == "2020-01-02T00:00:00Z" {
		t.Errorf("last-modified = %q, want the time of this rewrite", modified[0].Text())
	}
}

// D.1.4: a document may bind a reserved prefix to its own vocabulary, which
// D.1.5 only SHOULD NOTs. This file's dcterms:modified belongs to someone else
// and must not be read as the §5.5.5 date or overwritten.
func TestSpecRedefinedReservedPrefixIsNotOurProperty(t *testing.T) {
	opf := epub3(`    <meta property="dcterms:modified">not-a-date</meta>`).
		attr(`prefix="dcterms: http://example.com/vocab#"`)

	path := buildEpub(t, opf)
	want := "A New Title"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Title: &want}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	pkg := md.Parent()

	// Their property is untouched: we do not own http://example.com/vocab#modified.
	var theirs *etree.Element
	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "dcterms:modified" {
			theirs = m
		}
	}
	if theirs == nil {
		t.Fatal("the document's own dcterms:modified was removed")
	}
	if theirs.Text() != "not-a-date" {
		t.Errorf("their dcterms:modified = %q, want it untouched — it is not the DCMI property", theirs.Text())
	}

	// And a real last-modified date was added, under a prefix that resolves to
	// the DCMI vocabulary in this document.
	bindings := map[string]string{}
	fields := strings.Fields(pkg.SelectAttrValue("prefix", ""))
	for i := 0; i+1 < len(fields); i += 2 {
		bindings[strings.TrimSuffix(fields[i], ":")] = fields[i+1]
	}
	var found bool
	for _, m := range md.SelectElements("meta") {
		p := m.SelectAttrValue("property", "")
		prefix, local, ok := strings.Cut(p, ":")
		if !ok || local != "modified" {
			continue
		}
		if bindings[prefix] == "http://purl.org/dc/terms/" {
			found = true
			if _, err := time.Parse("2006-01-02T15:04:05Z", m.Text()); err != nil {
				t.Errorf("%s = %q, want the extended UTC format", p, m.Text())
			}
		}
	}
	if !found {
		t.Errorf("no last-modified property resolves to the DCMI vocabulary; prefix=%q",
			pkg.SelectAttrValue("prefix", ""))
	}
}

// D.1.4 for refinements. scheme="marc:relators" is a prefixed name, so a
// document rebinding marc turns it into a code list nobody meant. Spell it with
// whatever prefix resolves to MARC here.
func TestSpecNewRefineSpellsItsSchemeAndProperty(t *testing.T) {
	opf := epub3(``).attr(`prefix="marc: http://example.com/not-marc#"`)

	path := buildEpub(t, opf)
	authors := []bookmodel.Author{{Name: "Ann Rand"}, {Name: "Bo Li"}}
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	bindings := map[string]string{}
	fields := strings.Fields(md.Parent().SelectAttrValue("prefix", ""))
	for i := 0; i+1 < len(fields); i += 2 {
		bindings[strings.TrimSuffix(fields[i], ":")] = fields[i+1]
	}
	bindings["marc"] = bindings["marc"] // present or not, the doc's binding wins

	for _, m := range md.SelectElements("meta") {
		if m.SelectAttrValue("property", "") != "role" {
			continue
		}
		scheme := m.SelectAttrValue("scheme", "")
		if scheme == "" {
			continue
		}
		prefix, _, _ := strings.Cut(scheme, ":")
		url, declared := bindings[prefix]
		if !declared {
			url = "http://id.loc.gov/vocabulary/" // reserved, needs no declaration
		}
		if url != "http://id.loc.gov/vocabulary/" {
			t.Errorf("role scheme = %q, which resolves to %q, not the MARC relator vocabulary", scheme, url)
		}
	}
}

// --- a metadata value is text, not a path component ---
//
// §5.5.2 licenses one transformation of a value: strip and collapse whitespace.
// Substituting characters is not among them, and a writer that persists a
// substitution has destroyed the value.

func TestSpecSlashInAValueIsNotRewritten(t *testing.T) {
	path := buildEpub(t, epub3(`    <dc:title>Either/Or</dc:title>
    <dc:creator id="c1">AC/DC</dc:creator>`))

	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Either/Or" {
		t.Errorf("title = %q, want it read as written", bib.Title)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"AC/DC"}) {
		t.Errorf("authors = %v, want [AC/DC] as written", got)
	}

	// An edit to an unrelated field carries the author list back the way
	// library.Edit does, so a read-side substitution would reach the file.
	desc := "A new description."
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Description: &desc, Authors: &bib.Authors}); err != nil {
		t.Fatal(err)
	}
	raw := string(readEntry(t, path, opfPath))
	if !strings.Contains(raw, "AC/DC") {
		t.Errorf("the creator no longer says AC/DC:\n%s", raw)
	}
	if !strings.Contains(raw, "Either/Or") {
		t.Errorf("the title no longer says Either/Or:\n%s", raw)
	}
}

// --- dc-metadata / x-metadata ---
//
// OPF 2.0 §2.2 (Publication Metadata):
//   "Reading Systems must allow the specification of the deprecated dc-metadata
//    and x-metadata elements. ... If the dc-metadata element is used, all dc
//    elements must go in dc-metadata and all other metadata elements, if any,
//    must go into x-metadata."

func TestSpecLegacyMetadataWrappers(t *testing.T) {
	bib, err := epub.Parse(buildEpub(t, opfWrappers))
	if err != nil {
		t.Fatalf("reading systems MUST allow dc-metadata/x-metadata: %v", err)
	}
	if bib.Title != "Alice in Wonderland" {
		t.Errorf("title = %q", bib.Title)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v", got)
	}
	if bib.Series == nil || bib.Series.Name != "Alice" {
		t.Errorf("series = %+v, want Alice from x-metadata", bib.Series)
	}
}

// --- role, cardinality zero or more ---
//
// D.3.10 gives role cardinality "zero or more", with importance following
// document order. The test runs both orders: a creator with several roles
// including aut is an author either way, and a role we do not model survives an
// edit.

func TestSpecMultipleRoleRefines(t *testing.T) {

	opf := func(first, second string) packageDoc {
		return epub3(`    <dc:title>Where the Wild Things Are</dc:title>
    <dc:creator id="creator01">Maurice Sendak</dc:creator>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + first + `</meta>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + second + `</meta>
    <dc:language>en</dc:language>`)
	}

	for _, order := range [][2]string{{"ill", "aut"}, {"aut", "ill"}} {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			path := buildEpub(t, opf(order[0], order[1]))
			bib, err := epub.Parse(path)
			if err != nil {
				t.Fatalf("a creator with several roles including aut is an author: %v", err)
			}
			if got := authorNames(bib); !slices.Equal(got, []string{"Maurice Sendak"}) {
				t.Fatalf("authors = %v, want [Maurice Sendak]", got)
			}

			// A no-op author edit must not strip the role that is not ours.
			authors := []bookmodel.Author{{Name: "Maurice Sendak"}}
			if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
				t.Fatal(err)
			}
			md := metadata(t, path)
			var roles []string
			for _, m := range md.SelectElements("meta") {
				if m.SelectAttrValue("property", "") == "role" {
					roles = append(roles, m.Text())
				}
			}
			if !slices.Contains(roles, "ill") {
				t.Errorf("roles after a no-op edit = %v, want the ill credit kept", roles)
			}
		})
	}
}

// --- refines is a URL, not always "#id" ---
//
// EPUB 3.3 §5.3.6 (The refines attribute):
//   "EPUB creators MUST use as the value a path-relative-scheme-less-URL string,
//    optionally followed by U+0023 (#) and a URL-fragment string"
// so refines="content.opf#creator01" is conformant.

func TestSpecPathQualifiedRefines(t *testing.T) {

	var opf = epub3(`    <dc:title id="t1">The Title</dc:title>
    <meta refines="content.opf#t1" property="file-as">Title, The</meta>
    <dc:creator id="creator01">Lewis Carroll</dc:creator>
    <meta refines="content.opf#creator01" property="role" scheme="marc:relators">aut</meta>
    <meta refines="content.opf#creator01" property="file-as">Carroll, Lewis</meta>
    <dc:creator id="creator02">Sir John Tenniel</dc:creator>
    <meta refines="content.opf#creator02" property="role" scheme="marc:relators">ill</meta>
    <meta property="belongs-to-collection" id="c01">Alice</meta>
    <meta refines="content.opf#c01" property="collection-type">series</meta>
    <meta refines="content.opf#c01" property="group-position">2</meta>`)

	path := buildEpub(t, opf)
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.SortTitle != "Title, The" {
		t.Errorf("sort title = %q, want it resolved through a path-qualified refines", bib.SortTitle)
	}
	// Tenniel's role resolves to ill, so he is not an author. If the
	// path-qualified refines did not resolve, his role would be invisible and the
	// no-role default would promote him.
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll] only", got)
	}
	if len(bib.Authors) > 0 && bib.Authors[0].SortName != "Carroll, Lewis" {
		t.Errorf("author sort name = %q", bib.Authors[0].SortName)
	}
	if bib.Series == nil || bib.Series.Name != "Alice" || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want Alice at 2", bib.Series)
	}

	// D.3.6 file-as: "Cardinality: zero or one". An edit must not add a second
	// one beside the refine it failed to match.
	sort := "New, The"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{SortTitle: &sort}); err != nil {
		t.Fatal(err)
	}
	var fileAs int
	for _, m := range metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "file-as" &&
			strings.Contains(m.SelectAttrValue("refines", ""), "t1") {
			fileAs++
		}
	}
	if fileAs != 1 {
		t.Errorf("file-as refines on the title = %d, want exactly one per D.3.6", fileAs)
	}
}

// --- group-position ---
//
// EPUB 3.3 Appendix D.3.7 (group-position):
//   Allowed value(s): "A single xsd:unsignedInt or series of decimal-separated
//   numbers (e.g., 1 or 2.2.1)."
// A series of numbers, not one number: 2.2.1 has three levels and no numeric
// type holds it, so SeriesRef.Index is the string as written.

// articleAt is the fixture the three group-position tests share: one article in
// one series, at the position each of them is about.
func articleAt(position string) packageDoc {
	return epub3(metas(
		`<dc:title>An Article</dc:title>`,
		collection("c01", "Physical Review D", "series", position),
	))
}

func TestSpecGroupPositionMultiLevel(t *testing.T) {
	bib, err := epub.Parse(buildEpub(t, articleAt("2.2.1")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil {
		t.Fatal("series missing")
	}
	if bib.Series.Index != "2.2.1" {
		t.Errorf("index = %q, want the multi-level position kept exactly as written", bib.Series.Index)
	}
}

// D.3.7 counts levels, so "1.10" is volume 1, issue 10, not the number 1.1.
// calibre:series_index is a float and needs trailing zeros dropped ("1.0" means
// "1"). group-position is never calibre-written and must not get that treatment.
func TestSpecGroupPositionLevelsAreNotDecimals(t *testing.T) {
	bib, err := epub.Parse(buildEpub(t, articleAt("1.10")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.10" {
		t.Errorf("index = %+v, want 1.10 — issue 10 of volume 1, not 1.1", bib.Series)
	}

	// And it stays distinguishable from a genuine 1.1.
	bib, err = epub.Parse(buildEpub(t, articleAt("1.1")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.1" {
		t.Errorf("index = %+v, want 1.1", bib.Series)
	}
}

// A multi-level position must survive being set, not just being read. Writing
// it through a float was what silently collapsed it.
func TestSpecGroupPositionMultiLevelRoundTrips(t *testing.T) {
	path := buildEpub(t, articleAt("1"))
	want := "2.2.1" // no float holds this, which is the point
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{SeriesIndex: &want}); err != nil {
		t.Fatal(err)
	}
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != want {
		t.Errorf("series = %+v, want the position written back as %q", bib.Series, want)
	}
	if got := property(t, metadata(t, path), "//meta[@property='group-position']"); got != want {
		t.Errorf("group-position element = %q, want %q verbatim", got, want)
	}
}

// --- schemed collection-type ---
//
// EPUB 3.3 Appendix D.3.4 (collection-type):
//   "When the collection-type value is drawn from a code list or other formal
//    enumeration, EPUB creators SHOULD attach a scheme attribute to identify its
//    source. This specification also defines the following collection types when
//    no scheme is specified: series / set."
// So "series" under someone else's scheme is not the EPUB series type.

func TestSpecSchemedCollectionTypeIsNotOurSeries(t *testing.T) {

	var opf = epub3(`    <meta property="belongs-to-collection" id="c01">Acme Bundle</meta>
    <meta refines="#c01" property="collection-type" scheme="onix:codelist148">series</meta>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series != nil {
		t.Errorf("series = %+v, want nil — the collection-type is drawn from another scheme", bib.Series)
	}
}

// --- cover resolution ---
//
// §5.9.2: legacy features are for EPUB 2 compatibility, and "EPUB 3 reading
// systems will not use these features when presenting publications to users".
// That is what makes the manifest property win over <meta name="cover">.
// §5.9.3 describes the legacy meta itself.

// §5.9.1 makes properties "a space-separated list of property values", so
// membership is a token comparison. A substring test matches "my-cover-image",
// somebody else's property.
func TestSpecCoverImagePropertyIsAToken(t *testing.T) {
	opf := func(properties string) packageDoc {
		return epub3(`    <meta name="cover" content="legacy-cover"/>`).manifest(
			`<item id="legacy-cover" href="old.jpg" media-type="image/jpeg"/>
    <item id="candidate" href="candidate.jpg" media-type="image/jpeg" properties="` + properties + `"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`)
	}

	for _, tc := range []struct {
		name, properties, want string
	}{
		{"the property alone", "cover-image", "OEBPS/candidate.jpg"},
		{"one token among several", "svg cover-image scripted", "OEBPS/candidate.jpg"},
		{"a different property that contains it", "my-cover-image", "OEBPS/old.jpg"},
		{"a different property it is a prefix of", "cover-image-thumbnail", "OEBPS/old.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bib, err := epub.Parse(buildEpub(t, opf(tc.properties)))
			if err != nil {
				t.Fatal(err)
			}
			if bib.CoverPath != tc.want {
				t.Errorf("properties=%q gave cover %q, want %q", tc.properties, bib.CoverPath, tc.want)
			}
		})
	}
}

// Already correct, but stated only by the order of two loops in translateCover.
// Reorder them in a rewrite and the result flips with nothing failing.
func TestSpecCoverImagePropertyBeatsLegacyMeta(t *testing.T) {
	opf := epub3(`    <meta name="cover" content="legacy-cover"/>`).manifest(
		`<item id="legacy-cover" href="old.jpg" media-type="image/jpeg"/>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath != "OEBPS/cover.jpg" {
		t.Errorf("cover = %q, want the cover-image manifest item to win per §5.9.2", bib.CoverPath)
	}
}

// --- dc:date events ---
//
// OPF 2.0 §2.2.7: the opf:event vocabulary is open. "the set of values for
// event are not defined by this specification". We recognise only the literal
// "publication". Pinned so that closed-world reading changes on purpose.

func TestSpecUnrecognisedDateEventsLeaveNoPubdate(t *testing.T) {
	var opf = epub2(`    <dc:date opf:event="creation">1999-01-01</dc:date>
    <dc:date opf:event="original-publication">2000-01-01</dc:date>
    <dc:date opf:event="modification">2001-01-01</dc:date>`)

	bib, err := epub.Parse(buildEpub(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Pubdate != "" {
		t.Errorf("pubdate = %q; the closed-world reading changed — decide it deliberately", bib.Pubdate)
	}
}

// --- writing into the legacy wrappers ---
//
// OPF 2.0 §2.2 (Publication Metadata):
//   "If the dc-metadata element is used, all dc elements must go in dc-metadata
//    and all other metadata elements, if any, must go into x-metadata."

func TestSpecEditsLandInTheLegacyWrappers(t *testing.T) {
	path := buildEpub(t, opfWrappers)
	desc, series, index := "A new description.", "Wonderland", "3"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Description: &desc, Series: &series, SeriesIndex: &index}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	if got := textOf(t, md, "dc-metadata/description"); got != desc {
		t.Errorf("dc:description = %q, want %q inside dc-metadata per §2.2", got, desc)
	}
	// The series meta the file already had is updated where it sits; the index
	// is new, so it has to be created inside the wrapper rather than beside it.
	if got := legacyMeta(t, md, "x-metadata/meta[@name='calibre:series']"); got != series {
		t.Errorf("calibre:series = %q, want %q still inside x-metadata", got, series)
	}
	if got := legacyMeta(t, md, "x-metadata/meta[@name='calibre:series_index']"); got != index {
		t.Errorf("calibre:series_index = %q, want %q inside x-metadata", got, index)
	}
	for _, p := range []string{"description", "meta[@name='calibre:series']", "meta[@name='calibre:series_index']"} {
		if md.FindElement(p) != nil {
			t.Errorf("%s was written loose under <metadata>, outside the wrappers", p)
		}
	}

	// And the reader finds them where the writer put them.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Description != desc {
		t.Errorf("description = %q, want %q read back through dc-metadata", bib.Description, desc)
	}
	if bib.Series == nil || bib.Series.Name != series || bib.Series.Index != index {
		t.Errorf("series = %+v, want %q at %s read back through x-metadata", bib.Series, series, index)
	}
}

// dc-metadata present, x-metadata absent. §2.2's MUST still binds what gets
// added. elements() reads both wrappers and direct children, so this package is
// the one reader that cannot see its own violation.
func TestSpecEditsCreateTheMissingXMetadataWrapper(t *testing.T) {
	opf := epub2(metas(
		`<dc:title>Alice in Wonderland</dc:title>`,
		`<dc:creator opf:role="aut">Lewis Carroll</dc:creator>`,
	)).wrapped()

	path := buildEpub(t, opf)
	series := "Wonderland"
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Series: &series}); err != nil {
		t.Fatal(err)
	}

	md := metadata(t, path)
	if md.FindElement("x-metadata") == nil {
		t.Error("no x-metadata wrapper was created for the new meta")
	}
	if got := legacyMeta(t, md, "x-metadata/meta[@name='calibre:series']"); got != series {
		t.Errorf("calibre:series = %q, want %q inside x-metadata per §2.2", got, series)
	}
	if md.FindElement("meta[@name='calibre:series']") != nil {
		t.Error("calibre:series was written loose under <metadata>, outside the wrappers")
	}
}

// --- opf: attributes need a declared prefix ---
//
// XML Namespaces §6.2 (Namespace Defaulting) states that default namespaces do
// not apply directly to attributes.

func TestSpecEPUB2AttributesGetADeclaredPrefix(t *testing.T) {
	// epub2() declares xmlns:opf; this file binds OPF as the default only.
	opf := packageDoc(strings.Replace(string(epub2(``)), ` xmlns:opf="http://www.idpf.org/2007/opf"`, "", 1))

	path := buildEpub(t, opf)
	authors := []bookmodel.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := epub.Rewrite(path, book(t, path), edits.Edits{Authors: &authors}); err != nil {
		t.Fatal(err)
	}

	raw := string(readEntry(t, path, opfPath))
	if !strings.Contains(raw, `xmlns:opf="http://www.idpf.org/2007/opf"`) {
		t.Error("no xmlns:opf declaration was added for the prefixed attributes")
	}
	c := metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:role", ""); got != "aut" {
		t.Errorf("opf:role = %q, want aut", got)
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want the sort name", got)
	}

	// The reader resolves the attributes under the prefix the writer declared.
	bib, err := epub.Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want one author sorting as Rand, Ann", bib.Authors)
	}
}
