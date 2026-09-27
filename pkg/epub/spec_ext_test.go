// Conformance tests. Each cites the section it checks, so a failure means the
// package stopped conforming. Where a test also asserts something no spec
// requires, it says which half is ours.
//
// Fixtures are minimal, and conform only in the respect their test is about.
//
// https://www.w3.org/TR/epub-33/#app-meta-property-vocab
// https://idpf.org/epub/20/spec/OPF_2.0_final_spec.html

package epub_test

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/ramblingenzyme/ebookfs/internal/testing/epubtest"
	"github.com/ramblingenzyme/ebookfs/pkg/epub"
)

var opfTitleTypes = epubtest.EPUB3(`    <dc:title id="t1">The Complete Trilogy</dc:title>
    <meta refines="#t1" property="title-type">collection</meta>
    <dc:title id="t2">The Fellowship</dc:title>
    <meta refines="#t2" property="title-type">main</meta>
    <meta refines="#t2" property="file-as">Fellowship, The</meta>
    <dc:title id="t3">Being the First Part</dc:title>
    <meta refines="#t3" property="title-type">subtitle</meta>`)

// §5.5.3.1.2: the first dc:title in document order is the title, even when a
// later one is labelled "main". calibre picks the "main" one. Ours: replacing
// the title drops the other dc:title elements.
func TestSpecFirstTitleWins(t *testing.T) {
	path := epubtest.Build(t, opfTitleTypes)

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "The Complete Trilogy" {
		t.Errorf("title = %q, want the first dc:title per §5.5.3.1.2", bib.Title)
	}

	want := "A New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}
	md := epubtest.Metadata(t, path)
	if got := titleByID(md, "t1"); got != want {
		t.Errorf("first title = %q, want %q — read and write must resolve the same element", got, want)
	}

	// Left behind, t2 is what calibre would show as the title.
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

var opfDisplaySeq = epubtest.EPUB3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c1" property="display-seq">2</meta>
    <dc:creator id="c2">Bo Li</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">aut</meta>
    <meta refines="#c2" property="display-seq">1</meta>`)

// D.3.5: display-seq "only applies where precedence rules have not already been
// defined (e.g., precedence is given to creators based on their appearance in
// document order)". §5.5.3.2.3 and OPF 2.0 §2.2.2 define that order for creators.
func TestSpecCreatorOrderIsDocumentOrder(t *testing.T) {
	path := epubtest.Build(t, opfDisplaySeq)
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want document order [Ann Rand, Bo Li] per §5.5.3.2.3", got)
	}
}

// §5.5.5 requires exactly one dcterms:modified, in UTC and the extended format.
// Updating it on a change is only a lowercase "should" (§1.5), so that half is ours.
func TestSpecModifiedIsUpdated(t *testing.T) {

	path := epubtest.Build(t, epubtest.RichOPF3) // carries dcterms:modified 2020-01-02T00:00:00Z
	want := "A New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}
	got := epubtest.Metadata(t, path).FindElement("//meta[@property='dcterms:modified']")
	if got == nil {
		t.Fatal("dcterms:modified was removed")
	}
	if got.Text() == "2020-01-02T00:00:00Z" {
		t.Errorf("dcterms:modified = %q, want the time of this rewrite", got.Text())
	}
	if n := len(epubtest.Metadata(t, path).FindElements("//meta[@property='dcterms:modified']")); n != 1 {
		t.Errorf("dcterms:modified count = %d, want exactly one per §5.5.5", n)
	}
	if _, err := time.Parse("2006-01-02T15:04:05Z", got.Text()); err != nil {
		t.Errorf("dcterms:modified = %q, want the extended UTC format YYYY-MM-DDThh:mm:ssZ", got.Text())
	}
}

var opfCollections = epubtest.EPUB3(epubtest.Metas(
	epubtest.Collection("ps1", "Acme Classics", "publisher-series", ""),
	epubtest.Collection("set1", "Complete Works", "set", ""),
	epubtest.Collection("s1", "The Trilogy", "series", "2", "set1"),
))

// D.3.4 defines two collection-type values when no scheme is given, series and
// set, so "publisher-series" is neither. D.3.3 covers the nesting. Ours: a
// rename rewrites the series in place and leaves its parent alone.
func TestSpecOnlySeriesCollectionIsTheSeries(t *testing.T) {
	path := epubtest.Build(t, opfCollections)

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Name != "The Trilogy" || bib.Series.Index != "2" {
		t.Fatalf("series = %+v, want The Trilogy at 2", bib.Series)
	}

	want := "The Quartet"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, want) }); err != nil {
		t.Fatal(err)
	}
	md := epubtest.Metadata(t, path)

	for _, c := range []struct{ id, text string }{
		{"ps1", "Acme Classics"},
		{"set1", "Complete Works"},
	} {
		if got := epubtest.Property(t, md, "//meta[@id='"+c.id+"']"); got != c.text {
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

// Neither spec says an omitted role means author. That reading is ours, from dc:creator as the
// party "responsible for the creation of the content" (EPUB 3.3 §5.5.3.2.3) and
// "A primary creator or author of the publication" (OPF 2.0 §2.2.2).
func TestSpecOnlyAuthorRoleCreatorsAreAuthors(t *testing.T) {
	var opf = epubtest.EPUB3(`    <dc:creator id="c1">Ann Rand</dc:creator>
    <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
    <dc:creator id="c2">Acme Editorial Board</dc:creator>
    <meta refines="#c2" property="role" scheme="marc:relators">edt</meta>
    <dc:creator id="c3">Bo Li</dc:creator>`)

	path := epubtest.Build(t, opf)
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Ann Rand", "Bo Li"}) {
		t.Errorf("authors = %v, want [Ann Rand, Bo Li]", got)
	}

	authors := []epub.Author{{Name: "Ann Rand"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}
	if got := epubtest.TextOf(t, epubtest.Metadata(t, path), "//creator[@id='c2']"); got != "Acme Editorial Board" {
		t.Error("the editor was removed by an authors edit")
	}
}

// §5.5.3.1.3: the first dc:language is primary. OPF 2.0 §2.2.12 allows several
// and names no rule, so first-wins for EPUB 2 is ours.
func TestSpecFirstLanguageWins(t *testing.T) {
	var opf = epubtest.EPUB3(`    <dc:language>en</dc:language>
    <dc:language>fr</dc:language>`)

	path := epubtest.Build(t, opf)
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Language != "en" {
		t.Errorf("language = %q, want the first of en, fr", bib.Language)
	}

	fr := "de"
	if _, err := save(t, path, func(b *epub.Book) { b.Language = fr }); err != nil {
		t.Fatal(err)
	}
	langs := epubtest.Metadata(t, path).SelectElements("language")
	if len(langs) != 2 {
		t.Fatalf("dc:language count = %d, want 2", len(langs))
	}
	if langs[0].Text() != "de" || langs[1].Text() != "fr" {
		t.Errorf("languages = %q, %q, want de, fr", langs[0].Text(), langs[1].Text())
	}
}

// XML 1.0 §3.3.1: "ID values MUST uniquely identify the elements which bear them." §5.3.6 targets
// refines by fragment, so a duplicate id binds a refinement to both elements.
func TestSpecMintedIDsDoNotCollide(t *testing.T) {
	// Each fixture puts the id the writer would mint on a different kind of element.
	for _, tc := range []struct {
		name string
		meta string
		edit func(*epub.Book)
	}{
		{
			name: "title sort vs a squatted ebookfs-title",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title>The Title</dc:title>
    <dc:creator id="ebookfs-title">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edit: func(b *epub.Book) { b.SortTitle = "Title, The" },
		},
		{
			name: "new creator vs a squatted ebookfs-creator",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-creator">The Title</dc:title>
    <dc:creator>Ann Rand</dc:creator>
    <dc:language id="ebookfs-creator-1">en</dc:language>`,
			edit: func(b *epub.Book) { b.Authors = []epub.Author{{Name: "Someone Else"}} },
		},
		{
			name: "new collection vs a squatted ebookfs-series",
			meta: `    <dc:identifier id="pub-id">urn:uuid:1234</dc:identifier>
    <dc:title id="ebookfs-series">The Title</dc:title>
    <dc:creator id="c1">Ann Rand</dc:creator>
    <dc:language>en</dc:language>`,
			edit: func(b *epub.Book) { rename(b, "The Trilogy") },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := epubtest.Build(t, epubtest.EPUB3(tc.meta))
			if _, err := save(t, path, tc.edit); err != nil {
				t.Fatal(err)
			}
			assertUniqueIDs(t, path)
		})
	}
}

func TestSpecRepeatedEditsDoNotCollideIDs(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(``))

	names := []string{"Ann Rand"}
	for _, add := range []string{"Bo Carr", "Cy Dunn", "Di Ekko"} {
		names = append(names, add)
		authors := make([]epub.Author, len(names))
		for i, n := range names {
			authors[i] = epub.Author{Name: n}
		}
		if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
			t.Fatalf("adding %s: %v", add, err)
		}
		assertUniqueIDs(t, path)

		bib, err := parse(t, path)
		if err != nil {
			t.Fatalf("after adding %s: %v", add, err)
		}
		if got := authorNames(bib); !slices.Equal(got, names) {
			t.Fatalf("authors = %v, want %v", got, names)
		}
	}
}

// The new author's id is minted before the existing creator is placed, so a
// scan of placed creators alone would miss the clash.
func TestSpecReorderingAuthorsKeepsIDsUnique(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(`    <dc:creator id="ebookfs-creator">Alice</dc:creator>
    <meta refines="#ebookfs-creator" property="file-as">Alice, A</meta>`))

	authors := []epub.Author{{Name: "Bob", SortName: "Bob, B"}, {Name: "Alice", SortName: "Alice, A"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}
	assertUniqueIDs(t, path)

	bib, err := parse(t, path)
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

func assertUniqueIDs(t *testing.T, path string) {
	t.Helper()
	doc := etree.NewDocument()
	if err := doc.ReadFromBytes(epubtest.ReadEntry(t, path, epubtest.OPFPath)); err != nil {
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

// §5.5.2, on a fixture formatted the way the spec prints its own examples.
func TestSpecWhitespaceIsCollapsed(t *testing.T) {

	bib, err := parse(t, epubtest.Build(t, opfSpecStyleWhitespace))
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
	if got := bib.Identifiers()["uuid"]; got != "A1B0D67E" {
		t.Errorf("identifier = %q, want it collapsed and trimmed", got)
	}
}

// XML 1.0 §3.3.3 turns a newline inside an attribute value into a space without
// trimming it, so a wrapped opf:role arrives padded.
func TestSpecWhitespaceInEPUB2RoleAttribute(t *testing.T) {

	opf := epubtest.EPUB2(`    <dc:title>Alice in Wonderland</dc:title>
    <dc:creator opf:role=" aut " opf:file-as="Carroll, Lewis">Lewis Carroll</dc:creator>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatalf("a padded opf:role must still be an author role: %v", err)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll]", got)
	}
}

// Read as EPUB 2, a padded version="3.0" would skip the §5.5.5 update and gain calibre metas.
func TestSpecWhitespaceInTheVersionAttribute(t *testing.T) {
	opf := epubtest.PackageDoc(strings.Replace(string(epubtest.EPUB3(`    <meta property="dcterms:modified">2020-01-02T00:00:00Z</meta>`)),
		`version="3.0"`, "version=\"\n      3.0\n    \"", 1))

	path := epubtest.Build(t, opf)
	sort := "Title, The"
	if _, err := save(t, path, func(b *epub.Book) { b.SortTitle = sort }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
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

// D.1.4: "EPUB creators MUST declare the prefix mappings they use in the prefix
// attribute of the package element." D.1.5 reserves a set that need no declaration.
func TestSpecDeclaredPrefixResolvesToTheSameProperty(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <meta property="dct:modified">2020-01-02T00:00:00Z</meta>`, Attrs: `prefix="dct: http://purl.org/dc/terms/"`}.EPUB3()

	path := epubtest.Build(t, opf)
	want := "A New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}

	// §5.5.5: exactly one, whichever prefix spells it, and freshly written.
	var modified []*etree.Element
	for _, m := range epubtest.Metadata(t, path).SelectElements("meta") {
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
	opf := epubtest.Pkg{Meta: `    <meta property="dcterms:modified">not-a-date</meta>`, Attrs: `prefix="dcterms: http://example.com/vocab#"`}.EPUB3()

	path := epubtest.Build(t, opf)
	want := "A New Title"
	if _, err := save(t, path, func(b *epub.Book) { b.Title = want }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
	pkg := md.Parent()

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

// D.1.4 for refinements: in a document that rebinds marc,
// scheme="marc:relators" names someone else's code list.
func TestSpecNewRefineSpellsItsSchemeAndProperty(t *testing.T) {
	opf := epubtest.Pkg{Attrs: `prefix="marc: http://example.com/not-marc#"`}.EPUB3()

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand"}, {Name: "Bo Li"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
	bindings := map[string]string{}
	fields := strings.Fields(md.Parent().SelectAttrValue("prefix", ""))
	for i := 0; i+1 < len(fields); i += 2 {
		bindings[strings.TrimSuffix(fields[i], ":")] = fields[i+1]
	}

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

// §5.5.2 allows one transformation of a value: stripping and collapsing whitespace.
func TestSpecSlashInAValueIsNotRewritten(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(`    <dc:title>Either/Or</dc:title>
    <dc:creator id="c1">AC/DC</dc:creator>`))

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Title != "Either/Or" {
		t.Errorf("title = %q, want it read as written", bib.Title)
	}
	if got := authorNames(bib); !slices.Equal(got, []string{"AC/DC"}) {
		t.Errorf("authors = %v, want [AC/DC] as written", got)
	}

	// Passing the authors back, as library.Edit does, would write any read-side
	// substitution to the file.
	desc := "A new description."
	if _, err := save(t, path, func(b *epub.Book) { b.Description = desc; b.Authors = bib.Authors }); err != nil {
		t.Fatal(err)
	}
	raw := string(epubtest.ReadEntry(t, path, epubtest.OPFPath))
	if !strings.Contains(raw, "AC/DC") {
		t.Errorf("the creator no longer says AC/DC:\n%s", raw)
	}
	if !strings.Contains(raw, "Either/Or") {
		t.Errorf("the title no longer says Either/Or:\n%s", raw)
	}
}

// OPF 2.0 §2.2 (Publication Metadata):
//
//	"Reading Systems must allow the specification of the deprecated dc-metadata
//	 and x-metadata elements. ... If the dc-metadata element is used, all dc
//	 elements must go in dc-metadata and all other metadata elements, if any,
//	 must go into x-metadata."
func TestSpecLegacyMetadataWrappers(t *testing.T) {
	bib, err := parse(t, epubtest.Build(t, epubtest.OPFWrappers))
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

// D.3.10 gives role cardinality "zero or more", so aut in either position makes
// an author. Ours: a role we do not model survives an edit.
func TestSpecMultipleRoleRefines(t *testing.T) {

	opf := func(first, second string) epubtest.PackageDoc {
		return epubtest.EPUB3(`    <dc:title>Where the Wild Things Are</dc:title>
    <dc:creator id="creator01">Maurice Sendak</dc:creator>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + first + `</meta>
    <meta refines="#creator01" property="role" scheme="marc:relators">` + second + `</meta>
    <dc:language>en</dc:language>`)
	}

	for _, order := range [][2]string{{"ill", "aut"}, {"aut", "ill"}} {
		t.Run(order[0]+"-then-"+order[1], func(t *testing.T) {
			path := epubtest.Build(t, opf(order[0], order[1]))
			bib, err := parse(t, path)
			if err != nil {
				t.Fatalf("a creator with several roles including aut is an author: %v", err)
			}
			if got := authorNames(bib); !slices.Equal(got, []string{"Maurice Sendak"}) {
				t.Fatalf("authors = %v, want [Maurice Sendak]", got)
			}

			authors := []epub.Author{{Name: "Maurice Sendak"}}
			if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
				t.Fatal(err)
			}
			md := epubtest.Metadata(t, path)
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

// EPUB 3.3 §5.3.6 (The refines attribute):
//
//	"EPUB creators MUST use as the value a path-relative-scheme-less-URL string,
//	 optionally followed by U+0023 (#) and a URL-fragment string"
//
// so refines="content.opf#creator01" is conformant.
func TestSpecPathQualifiedRefines(t *testing.T) {

	var opf = epubtest.EPUB3(`    <dc:title id="t1">The Title</dc:title>
    <meta refines="content.opf#t1" property="file-as">Title, The</meta>
    <dc:creator id="creator01">Lewis Carroll</dc:creator>
    <meta refines="content.opf#creator01" property="role" scheme="marc:relators">aut</meta>
    <meta refines="content.opf#creator01" property="file-as">Carroll, Lewis</meta>
    <dc:creator id="creator02">Sir John Tenniel</dc:creator>
    <meta refines="content.opf#creator02" property="role" scheme="marc:relators">ill</meta>
    <meta property="belongs-to-collection" id="c01">Alice</meta>
    <meta refines="content.opf#c01" property="collection-type">series</meta>
    <meta refines="content.opf#c01" property="group-position">2</meta>`)

	path := epubtest.Build(t, opf)
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.SortTitle != "Title, The" {
		t.Errorf("sort title = %q, want it resolved through a path-qualified refines", bib.SortTitle)
	}
	// If the refines did not resolve, Tenniel would have no role and count as an author.
	if got := authorNames(bib); !slices.Equal(got, []string{"Lewis Carroll"}) {
		t.Errorf("authors = %v, want [Lewis Carroll] only", got)
	}
	if len(bib.Authors) > 0 && bib.Authors[0].SortName != "Carroll, Lewis" {
		t.Errorf("author sort name = %q", bib.Authors[0].SortName)
	}
	if bib.Series == nil || bib.Series.Name != "Alice" || bib.Series.Index != "2" {
		t.Errorf("series = %+v, want Alice at 2", bib.Series)
	}

	// D.3.6 file-as: "Cardinality: zero or one".
	sort := "New, The"
	if _, err := save(t, path, func(b *epub.Book) { b.SortTitle = sort }); err != nil {
		t.Fatal(err)
	}
	var fileAs int
	for _, m := range epubtest.Metadata(t, path).SelectElements("meta") {
		if m.SelectAttrValue("property", "") == "file-as" &&
			strings.Contains(m.SelectAttrValue("refines", ""), "t1") {
			fileAs++
		}
	}
	if fileAs != 1 {
		t.Errorf("file-as refines on the title = %d, want exactly one per D.3.6", fileAs)
	}
}

func articleAt(position string) epubtest.PackageDoc {
	return epubtest.EPUB3(epubtest.Metas(
		`<dc:title>An Article</dc:title>`,
		epubtest.Collection("c01", "Physical Review D", "series", position),
	))
}

// EPUB 3.3 Appendix D.3.7 (group-position):
//
//	Allowed value(s): "A single xsd:unsignedInt or series of decimal-separated
//	numbers (e.g., 1 or 2.2.1)."
func TestSpecGroupPositionMultiLevel(t *testing.T) {
	bib, err := parse(t, epubtest.Build(t, articleAt("2.2.1")))
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
// calibre:series_index drops trailing zeros; group-position must not.
func TestSpecGroupPositionLevelsAreNotDecimals(t *testing.T) {
	bib, err := parse(t, epubtest.Build(t, articleAt("1.10")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.10" {
		t.Errorf("index = %+v, want 1.10 — issue 10 of volume 1, not 1.1", bib.Series)
	}

	bib, err = parse(t, epubtest.Build(t, articleAt("1.1")))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != "1.1" {
		t.Errorf("index = %+v, want 1.1", bib.Series)
	}
}

func TestSpecGroupPositionMultiLevelRoundTrips(t *testing.T) {
	path := epubtest.Build(t, articleAt("1"))
	want := "2.2.1" // no float holds this, which is the point
	if _, err := save(t, path, func(b *epub.Book) { reposition(b, want) }); err != nil {
		t.Fatal(err)
	}
	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series == nil || bib.Series.Index != want {
		t.Errorf("series = %+v, want the position written back as %q", bib.Series, want)
	}
	if got := epubtest.Property(t, epubtest.Metadata(t, path), "//meta[@property='group-position']"); got != want {
		t.Errorf("group-position element = %q, want %q verbatim", got, want)
	}
}

// EPUB 3.3 Appendix D.3.4 (collection-type):
//
//	"When the collection-type value is drawn from a code list or other formal
//	 enumeration, EPUB creators SHOULD attach a scheme attribute to identify its
//	 source. This specification also defines the following collection types when
//	 no scheme is specified: series / set."
//
// So "series" under someone else's scheme is not the EPUB series type.
func TestSpecSchemedCollectionTypeIsNotOurSeries(t *testing.T) {

	var opf = epubtest.EPUB3(`    <meta property="belongs-to-collection" id="c01">Acme Bundle</meta>
    <meta refines="#c01" property="collection-type" scheme="onix:codelist148">series</meta>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Series != nil {
		t.Errorf("series = %+v, want nil — the collection-type is drawn from another scheme", bib.Series)
	}
}

// §5.9.1 makes properties "a space-separated list of property values".
func TestSpecCoverImagePropertyIsAToken(t *testing.T) {
	opf := func(properties string) epubtest.PackageDoc {
		return epubtest.Pkg{Meta: `    <meta name="cover" content="legacy-cover"/>`, Manifest: `<item id="legacy-cover" href="old.jpg" media-type="image/jpeg"/>
    <item id="candidate" href="candidate.jpg" media-type="image/jpeg" properties="` + properties + `"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()
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
			bib, err := parse(t, epubtest.Build(t, opf(tc.properties)))
			if err != nil {
				t.Fatal(err)
			}
			if bib.CoverPath() != tc.want {
				t.Errorf("properties=%q gave cover %q, want %q", tc.properties, bib.CoverPath(), tc.want)
			}
		})
	}
}

// §5.9.2: "EPUB 3 reading systems will not use these features when presenting
// publications to users". §5.9.3 describes the legacy meta.
func TestSpecCoverImagePropertyBeatsLegacyMeta(t *testing.T) {
	opf := epubtest.Pkg{Meta: `    <meta name="cover" content="legacy-cover"/>`, Manifest: `<item id="legacy-cover" href="old.jpg" media-type="image/jpeg"/>
    <item id="cover-img" href="cover.jpg" media-type="image/jpeg" properties="cover-image"/>
    <item id="ch1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>`}.EPUB3()

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.CoverPath() != "OEBPS/cover.jpg" {
		t.Errorf("cover = %q, want the cover-image manifest item to win per §5.9.2", bib.CoverPath())
	}
}

// OPF 2.0 §2.2.7: "the set of values for event are not defined by this
// specification". Ours: only "publication" is recognised.
func TestSpecUnrecognisedDateEventsLeaveNoPubdate(t *testing.T) {
	var opf = epubtest.EPUB2(`    <dc:date opf:event="creation">1999-01-01</dc:date>
    <dc:date opf:event="original-publication">2000-01-01</dc:date>
    <dc:date opf:event="modification">2001-01-01</dc:date>`)

	bib, err := parse(t, epubtest.Build(t, opf))
	if err != nil {
		t.Fatal(err)
	}
	if bib.Pubdate() != "" {
		t.Errorf("pubdate = %q; the closed-world reading changed — decide it deliberately", bib.Pubdate())
	}
}

// OPF 2.0 §2.2 (Publication Metadata):
//
//	"If the dc-metadata element is used, all dc elements must go in dc-metadata
//	 and all other metadata elements, if any, must go into x-metadata."
func TestSpecEditsLandInTheLegacyWrappers(t *testing.T) {
	path := epubtest.Build(t, epubtest.OPFWrappers)
	desc, series, index := "A new description.", "Wonderland", "3"
	if _, err := save(t, path, func(b *epub.Book) { b.Description = desc; b.Series = &epub.Series{Name: series, Index: index} }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
	if got := epubtest.TextOf(t, md, "dc-metadata/description"); got != desc {
		t.Errorf("dc:description = %q, want %q inside dc-metadata per §2.2", got, desc)
	}
	if got := epubtest.LegacyMeta(t, md, "x-metadata/meta[@name='calibre:series']"); got != series {
		t.Errorf("calibre:series = %q, want %q still inside x-metadata", got, series)
	}
	if got := epubtest.LegacyMeta(t, md, "x-metadata/meta[@name='calibre:series_index']"); got != index {
		t.Errorf("calibre:series_index = %q, want %q inside x-metadata", got, index)
	}
	for _, p := range []string{"description", "meta[@name='calibre:series']", "meta[@name='calibre:series_index']"} {
		if md.FindElement(p) != nil {
			t.Errorf("%s was written loose under <metadata>, outside the wrappers", p)
		}
	}

	bib, err := parse(t, path)
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

// §2.2 binds what an edit adds, and this package's own reader would not notice
// a meta written outside the wrappers.
func TestSpecEditsCreateTheMissingXMetadataWrapper(t *testing.T) {
	path := epubtest.Build(t, epubtest.DCMetadataOnly)
	series := "Wonderland"
	if _, err := save(t, path, func(b *epub.Book) { rename(b, series) }); err != nil {
		t.Fatal(err)
	}

	md := epubtest.Metadata(t, path)
	if md.FindElement("x-metadata") == nil {
		t.Error("no x-metadata wrapper was created for the new meta")
	}
	if got := epubtest.LegacyMeta(t, md, "x-metadata/meta[@name='calibre:series']"); got != series {
		t.Errorf("calibre:series = %q, want %q inside x-metadata per §2.2", got, series)
	}
	if md.FindElement("meta[@name='calibre:series']") != nil {
		t.Error("calibre:series was written loose under <metadata>, outside the wrappers")
	}
}

// XML Namespaces §6.2: default namespaces do not apply to attributes.
func TestSpecEPUB2AttributesGetADeclaredPrefix(t *testing.T) {
	// epubtest.EPUB2() declares xmlns:opf; this file binds OPF as the default only.
	opf := epubtest.PackageDoc(strings.Replace(string(epubtest.EPUB2(``)), ` xmlns:opf="http://www.idpf.org/2007/opf"`, "", 1))

	path := epubtest.Build(t, opf)
	authors := []epub.Author{{Name: "Ann Rand", SortName: "Rand, Ann"}}
	if _, err := save(t, path, func(b *epub.Book) { b.Authors = authors }); err != nil {
		t.Fatal(err)
	}

	raw := string(epubtest.ReadEntry(t, path, epubtest.OPFPath))
	if !strings.Contains(raw, `xmlns:opf="http://www.idpf.org/2007/opf"`) {
		t.Error("no xmlns:opf declaration was added for the prefixed attributes")
	}
	c := epubtest.Metadata(t, path).FindElement("creator")
	if c == nil {
		t.Fatal("creator was removed")
	}
	if got := c.SelectAttrValue("opf:role", ""); got != "aut" {
		t.Errorf("opf:role = %q, want aut", got)
	}
	if got := c.SelectAttrValue("opf:file-as", ""); got != "Rand, Ann" {
		t.Errorf("opf:file-as = %q, want the sort name", got)
	}

	bib, err := parse(t, path)
	if err != nil {
		t.Fatal(err)
	}
	if len(bib.Authors) != 1 || bib.Authors[0].SortName != "Rand, Ann" {
		t.Errorf("authors = %+v, want one author sorting as Rand, Ann", bib.Authors)
	}
}

// §5.5.2 makes a nameless collection invalid, so the read falls through to the
// calibre metas. Ours: whatever Series reports, a write keeps.
func TestSpecSeriesReportedIsSeriesWritable(t *testing.T) {
	path := epubtest.Build(t, epubtest.EPUB3(epubtest.Metas(
		epubtest.Collection("c01", "", "series", ""),
		epubtest.CalibreSeries("The Trilogy", "3"),
	)))

	if got := open(t, path).Series; got == nil || got.Name != "The Trilogy" {
		t.Fatalf("series = %+v, want The Trilogy from the calibre metas", got)
	}

	b, err := save(t, path, func(b *epub.Book) { reposition(b, "5") })
	if err != nil {
		t.Fatal(err)
	}
	if b.Series == nil {
		t.Fatal("a position edit deleted the series the read reported")
	}
	if b.Series.Name != "The Trilogy" || b.Series.Index != "5" {
		t.Errorf("series = %+v, want The Trilogy at 5", b.Series)
	}

	if got := open(t, path).Series; got == nil || got.Name != "The Trilogy" || got.Index != "5" {
		t.Errorf("series on disk = %+v, want The Trilogy at 5", got)
	}
}
