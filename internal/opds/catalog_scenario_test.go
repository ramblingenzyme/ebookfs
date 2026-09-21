// The catalog driven the way an OPDS reader drives it: an HTTP request in, a
// parsed Atom document out. Every layer between the URL and the library is the
// production one, including the upstream handler's routing and negotiation.
// Only the library and the exporter are faked.
//
// What these pin, which no single source file holds: that a href the catalog
// writes into one feed routes back to the query it names when a client follows
// it, that a facet value with a slash or a space survives that round trip, that
// a cover is read from the original epub rather than the export rendition, and
// that a download keeps the range support a reader resumes on.
package opds

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/internal/testing/mock"
	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library"
)

type atomFeed struct {
	XMLName xml.Name   `xml:"feed"`
	Title   string     `xml:"title"`
	Links   []atomLink `xml:"link"`
	Entries []struct {
		Title   string     `xml:"title"`
		ID      string     `xml:"id"`
		Content string     `xml:"content"`
		Links   []atomLink `xml:"link"`
		Authors []struct {
			Name string `xml:"name"`
		} `xml:"author"`
		Categories []struct {
			Term string `xml:"term,attr"`
		} `xml:"category"`
	} `xml:"entry"`
}

type atomLink struct {
	Rel  string `xml:"rel,attr"`
	Href string `xml:"href,attr"`
	Type string `xml:"type,attr"`
}

func (f atomFeed) href(rel string) string {
	for _, l := range f.Links {
		if l.Rel == rel {
			return l.Href
		}
	}
	return ""
}

func (f atomFeed) titles() []string {
	out := make([]string, len(f.Entries))
	for i, e := range f.Entries {
		out[i] = e.Title
	}
	return out
}

type fake struct {
	mock.Catalog
	queries []library.Query
	opened  int
}

// epubBody's length is the Content-Length the download assertions expect.
const epubBody = "PK\x03\x04 pretend this is an epub"

// newFake derives the facet listings from books, so a nav feed and the feed it
// links to cannot disagree.
func newFake(t *testing.T, books ...*library.Book) (*fake, http.Handler) {
	t.Helper()
	f := &fake{}
	f.SearchFn = func(q library.Query) ([]*library.Book, error) {
		f.queries = append(f.queries, q)
		return filter(books, q), nil
	}
	f.GetFn = func(id int64) (*library.Book, error) {
		for _, b := range books {
			if b.ID() == id {
				return b, nil
			}
		}
		return nil, fmt.Errorf("book %d: %w", id, library.ErrBookNotFound)
	}
	f.ContentFn = func(int64) (library.EpubReader, error) {
		return &mock.EpubReader{
			Reader:  bytes.NewReader([]byte(epubBody)),
			CoverFn: func() ([]byte, error) { return []byte("\x89PNG\r\n\x1a\n cover"), nil },
		}, nil
	}
	f.AuthorsFn = func() ([]library.Facet, error) { return count(books, authorsOf), nil }
	f.SeriesFn = func() ([]library.Facet, error) { return count(books, seriesOf), nil }
	f.TagsFn = func() ([]library.Facet, error) { return count(books, (*library.Book).Tags), nil }

	exp := mock.Exporter{Renderer: mock.Renderer{
		OpenFn: func(*library.Book) (library.EpubReader, error) {
			f.opened++
			return &mock.EpubReader{Reader: bytes.NewReader([]byte(epubBody))}, nil
		},
		SizeFn: func(*library.Book) (int64, bool) { return int64(len(epubBody)), true },
	}}
	return f, NewHandler(f, exp, "https://books.example.com")
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

func feed(t *testing.T, h http.Handler, path string) atomFeed {
	t.Helper()
	w := get(t, h, path)
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200\n%s", path, w.Code, w.Body)
	}
	var f atomFeed
	if err := xml.Unmarshal(w.Body.Bytes(), &f); err != nil {
		t.Fatalf("GET %s: parsing feed: %v\n%s", path, err, w.Body)
	}
	return f
}

// The root feed names every section, and each href it writes resolves to a
// feed rather than a 404, which catches a section renamed on one side of the
// id scheme only.
func TestRootFeedSectionsAllResolve(t *testing.T) {
	b := util.MakeMutableBook(1, "Dune", "Frank Herbert")
	b.Meta.Tags = []string{"scifi"}
	b.Series = &book.SeriesRef{Name: "Dune", Index: "1"}
	_, h := newFake(t, util.WrapBook(b))

	root := feed(t, h, Prefix+"/")
	want := []string{"All Books", "Recently Added", "Authors", "Series", "Tags", "Reading Status"}
	if got := root.titles(); !slices.Equal(got, want) {
		t.Fatalf("root sections = %v, want %v", got, want)
	}
	for _, e := range root.Entries {
		href := e.Links[0].Href
		if code := get(t, h, href).Code; code != http.StatusOK {
			t.Errorf("%s -> %s: status %d, want 200", e.Title, href, code)
		}
	}
}

// Following a facet listing reaches the books behind that value. The names
// carry a slash and a space because a href is a path segment and both have
// broken one before.
func TestFacetHrefRoundTripsAwkwardNames(t *testing.T) {
	b := util.MakeMutableBook(1, "Either/Or", "Søren Kierkegaard")
	b.Meta.Tags = []string{"philosophy/ethics"}
	f, h := newFake(t, util.WrapBook(b))

	tags := feed(t, h, Prefix+"/feed/tag")
	if got := tags.titles(); !slices.Equal(got, []string{"philosophy/ethics"}) {
		t.Fatalf("tag listing = %v", got)
	}
	if got := tags.Entries[0].Content; got != "1 book" {
		t.Errorf("count = %q, want %q", got, "1 book")
	}

	f.queries = nil
	books := feed(t, h, tags.Entries[0].Links[0].Href)
	if got := books.titles(); !slices.Equal(got, []string{"Either/Or"}) {
		t.Fatalf("books behind the tag = %v", got)
	}
	if len(f.queries) != 1 || !slices.Equal(f.queries[0].Tags, []string{"philosophy/ethics"}) {
		t.Errorf("query = %+v, want Tags [philosophy/ethics]", f.queries)
	}
}

// An entry carries what a reader needs to fetch the book: an acquisition link
// at the epub media type, a cover, the authors and the tags.
func TestEntryCarriesAcquisitionAndMetadata(t *testing.T) {
	b := util.MakeMutableBook(7, "Dune", "Frank Herbert")
	b.EpubPath = "Frank Herbert/Dune (7)/Dune.epub"
	b.CoverPath = "OEBPS/cover.jpg"
	b.Meta.Tags = []string{"scifi"}
	_, h := newFake(t, util.WrapBook(b))

	all := feed(t, h, Prefix+"/feed/all")
	if len(all.Entries) != 1 {
		t.Fatalf("entries = %d, want 1", len(all.Entries))
	}
	e := all.Entries[0]
	if e.ID != "urn:ebookfs:book:7" {
		t.Errorf("atom:id = %q", e.ID)
	}
	if len(e.Authors) != 1 || e.Authors[0].Name != "Frank Herbert" {
		t.Errorf("authors = %+v", e.Authors)
	}
	if len(e.Categories) != 1 || e.Categories[0].Term != "scifi" {
		t.Errorf("categories = %+v", e.Categories)
	}

	var acquisition, cover string
	for _, l := range e.Links {
		switch l.Rel {
		case "http://opds-spec.org/acquisition/open-access":
			acquisition = l.Href
			if l.Type != mediaTypeEpub {
				t.Errorf("acquisition type = %q, want %q", l.Type, mediaTypeEpub)
			}
		case "http://opds-spec.org/image":
			cover = l.Href
		}
	}
	if acquisition != Prefix+"/content/7/Dune.epub" {
		t.Errorf("acquisition href = %q", acquisition)
	}
	if cover != Prefix+"/cover/7" {
		t.Errorf("cover href = %q", cover)
	}
}

// A download serves the rendition with the length and range support a reader
// resumes on.
func TestDownloadSupportsRanges(t *testing.T) {
	b := util.MakeMutableBook(7, "Dune", "Frank Herbert")
	b.EpubPath = "Frank Herbert/Dune (7)/Dune.epub"
	_, h := newFake(t, util.WrapBook(b))

	w := get(t, h, Prefix+"/content/7/Dune.epub")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if got := w.Body.String(); got != epubBody {
		t.Errorf("body = %q", got)
	}
	if got := w.Header().Get("Content-Type"); got != mediaTypeEpub {
		t.Errorf("content type = %q", got)
	}
	if got := w.Header().Get("Content-Disposition"); !strings.Contains(got, "Dune.epub") {
		t.Errorf("content disposition = %q", got)
	}

	req := httptest.NewRequest(http.MethodGet, Prefix+"/content/7/Dune.epub", nil)
	req.Header.Set("Range", "bytes=2-5")
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusPartialContent {
		t.Fatalf("range status %d, want 206", rw.Code)
	}
	if got := rw.Body.String(); got != epubBody[2:6] {
		t.Errorf("range body = %q, want %q", got, epubBody[2:6])
	}
}

// The cover comes from the original epub. Reading it through the exporter
// would convert the book, which is minutes of CPU for a thumbnail.
func TestCoverBypassesTheExporter(t *testing.T) {
	b := util.MakeMutableBook(7, "Dune", "Frank Herbert")
	b.CoverPath = "OEBPS/cover.png"
	f, h := newFake(t, util.WrapBook(b))

	w := get(t, h, Prefix+"/cover/7")
	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "image/png" {
		t.Errorf("content type = %q, want image/png", got)
	}
	if f.opened != 0 {
		t.Errorf("exporter opened %d times, want 0", f.opened)
	}
}

// A coverless book is ordinary, so its cover route answers 404 rather than
// 500. Same for a book and a feed that do not exist.
// An author with no books is an empty feed instead: the catalog cannot tell
// that from a name nobody queried, and neither can a client.
func TestMissesAre404(t *testing.T) {
	b := util.MakeMutableBook(7, "Dune", "Frank Herbert")
	f, h := newFake(t, util.WrapBook(b))
	f.ContentFn = func(int64) (library.EpubReader, error) {
		return &mock.EpubReader{
			Reader:  bytes.NewReader(nil),
			CoverFn: func() ([]byte, error) { return nil, library.ErrNoCover },
		}, nil
	}

	cases := map[string]int{
		Prefix + "/cover/7":                 http.StatusNotFound,
		Prefix + "/cover/999":               http.StatusNotFound,
		Prefix + "/content/999/x.epub":      http.StatusNotFound,
		Prefix + "/feed/nonsense":           http.StatusNotFound,
		Prefix + "/feed/author:Nobody%20Xx": http.StatusOK,
	}
	for path, want := range cases {
		if code := get(t, h, path).Code; code != want {
			t.Errorf("GET %s: status %d, want %d", path, code, want)
		}
	}
}

// A library longer than one page hands the client a next link, and following
// it reaches the rest.
func TestPaginationLinksTheNextPage(t *testing.T) {
	books := make([]*library.Book, pageSize+3)
	for i := range books {
		books[i] = util.MakeBook(int64(i+1), fmt.Sprintf("Book %03d", i+1))
	}
	_, h := newFake(t, books...)

	first := feed(t, h, Prefix+"/feed/all")
	if len(first.Entries) != pageSize {
		t.Fatalf("first page = %d entries, want %d", len(first.Entries), pageSize)
	}
	next := first.href("next")
	if next == "" {
		t.Fatal("no next link on a feed with more pages")
	}
	second := feed(t, h, next)
	if len(second.Entries) != 3 {
		t.Errorf("second page = %d entries, want 3", len(second.Entries))
	}
	if second.href("next") != "" {
		t.Error("last page advertises a next page")
	}
}

// Search maps the OpenSearch terms onto a title query, and the description
// document advertises the endpoint the handler actually serves.
func TestSearchQueriesTitles(t *testing.T) {
	f, h := newFake(t, util.MakeBook(1, "Dune"))

	results := feed(t, h, Prefix+"/search?q=Dun")
	if got := results.titles(); !slices.Equal(got, []string{"Dune"}) {
		t.Fatalf("results = %v", got)
	}
	if len(f.queries) != 1 || !slices.Equal(f.queries[0].Titles, []string{"Dun"}) {
		t.Fatalf("query = %+v, want Titles [Dun]", f.queries)
	}
	if f.queries[0].ExactTitles {
		t.Error("search asked for exact titles; browsing wants the substring")
	}

	w := get(t, h, Prefix+"/opensearch.xml")
	if w.Code != http.StatusOK {
		t.Fatalf("opensearch: status %d", w.Code)
	}
	// OpenSearch 1.1 wants an absolute template, and the upstream handler only
	// absolutizes one it built itself, so this is the catalog's own job.
	if want := "https://books.example.com" + Prefix + "/search?q={searchTerms}"; !strings.Contains(w.Body.String(), want) {
		t.Errorf("opensearch template is not %q:\n%s", want, w.Body)
	}
}

// filter is the fake index, and selects over only the fields the catalog's own
// feeds set.
func filter(books []*library.Book, q library.Query) []*library.Book {
	var out []*library.Book
	for _, b := range books {
		if !matches(q.Authors, authorsOf(b)) || !matches(q.Tags, b.Tags()) ||
			!matches(q.Series, seriesOf(b)) || !matches(q.Status, []string{b.Status()}) {
			continue
		}
		if len(q.Titles) > 0 && !strings.Contains(b.Title(), q.Titles[0]) {
			continue
		}
		out = append(out, b)
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out
}

// matches treats an empty want as matching everything, which is how
// index.Search treats an unset field.
func matches(want, have []string) bool {
	if len(want) == 0 {
		return true
	}
	for _, w := range want {
		if slices.Contains(have, w) {
			return true
		}
	}
	return false
}

func authorsOf(b *library.Book) []string {
	out := make([]string, 0, len(b.Authors()))
	for _, a := range b.Authors() {
		out = append(out, a.Name)
	}
	return out
}

func seriesOf(b *library.Book) []string {
	if !b.HasSeries() {
		return nil
	}
	return []string{b.SeriesName()}
}

// count builds a facet listing the way the index does, one entry per distinct
// value.
func count(books []*library.Book, values func(*library.Book) []string) []library.Facet {
	var out []library.Facet
	seen := map[string]int{}
	for _, b := range books {
		for _, v := range values(b) {
			if i, ok := seen[v]; ok {
				out[i].Count++
				continue
			}
			seen[v] = len(out)
			out = append(out, library.Facet{Name: v, Count: 1})
		}
	}
	return out
}
