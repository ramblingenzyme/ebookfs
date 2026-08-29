package index

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/library/internal/index/dbsqlc"
	"github.com/ramblingenzyme/ebookfs/library/model"
)

// bookPaths pairs books with zero file times for Rebuild. Tests that don't
// exercise drift detection don't care what mtimes get recorded.
func bookPaths(books ...*book.Book) []BookPath {
	bts := make([]BookPath, len(books))
	for i, b := range books {
		bts[i] = BookPath{Book: b}
	}
	return bts
}

// openTestIndex returns a fresh index at a temp path, rebuilt to a clean,
// version-stamped baseline so tests start from a known clean state.
func openTestIndex(t *testing.T) *Index {
	t.Helper()
	idx, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { idx.Close() })
	// A fresh index is intentionally "dirty" (see TestFreshOpenForcesReindex);
	// rebuild an empty index to reach the clean baseline the mutation tests want.
	if err := idx.Rebuild(nil, nil, 0); err != nil {
		t.Fatalf("baseline rebuild: %v", err)
	}
	return idx
}

// TestPathInfoRoundTrip pins the nanosecond encoding: every other test passes a
// zero drift.PathInfo, so without this a broken encode/decode would surface only as
// drift detection quietly rebuilding on every startup.
//
// Both write paths are covered because each maps the observation onto its own
// query params — Put through UpsertBook, Rebuild through InsertBook — so an
// encoding fixed in one can stay broken in the other.
func TestPathInfoRoundTrip(t *testing.T) {
	writers := map[string]func(*testing.T, *Index, *book.Book, drift.PathInfo){
		"put": func(t *testing.T, idx *Index, b *book.Book, pi drift.PathInfo) {
			op := idx.BeginOp()
			if err := op.MarkPending(); err != nil {
				t.Fatalf("MarkPending: %v", err)
			}
			if err := op.Put(b, pi); err != nil {
				t.Fatalf("Put: %v", err)
			}
		},
		"rebuild": func(t *testing.T, idx *Index, b *book.Book, pi drift.PathInfo) {
			if err := idx.Rebuild([]BookPath{{Book: b, Info: pi}}, nil, b.Meta.ID); err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
		},
	}
	tests := []struct {
		name string
		want drift.PathInfo
	}{
		// A whole second (zero nanoseconds) alongside sub-millisecond precision
		// that a second-granularity format would silently truncate.
		{"recorded", drift.PathInfo{
			Size:      4242,
			EpubMtime: time.Unix(1700000000, 0),
			MetaSize:  17,
			MetaMtime: time.Unix(1700000000, 123456789),
		}},
		// Never observed: stores as 0 and must decode back to the zero time, not
		// to the Unix epoch, which would read as a real (and wrong) timestamp.
		{"zero", drift.PathInfo{}},
	}
	for writer, write := range writers {
		for _, tc := range tests {
			t.Run(writer+"/"+tc.name, func(t *testing.T) {
				idx := openTestIndex(t)
				write(t, idx, newBook(1, tc.name), tc.want)

				all, err := idx.AllPathInfo()
				if err != nil {
					t.Fatalf("AllPathInfo: %v", err)
				}
				got, ok := all[tc.name+"/book.epub"]
				if !ok {
					t.Fatalf("AllPathInfo missing %q, got %v", tc.name, all)
				}
				if !got.Equal(tc.want) {
					t.Errorf("drift.PathInfo = %+v, want %+v", got, tc.want)
				}
			})
		}
	}
}

// TestEpubSizeComesFromObservation pins the collapse of the epub's two size
// columns into one: books.epub_size is written from the observation handed to
// Put, and a size set on the book itself is ignored. The book's copy is what 9P
// reports as the file's length and what export sizing uses, while the drift
// check compares the stat's — so a second column here means those two can
// silently disagree, which is what this replaced.
func TestEpubSizeComesFromObservation(t *testing.T) {
	idx := openTestIndex(t)

	b := newBook(1, "Sized")
	b.EpubSize = 7 // discarded: the observation below is what lands in the row
	storeInIndexSized(t, idx, b, 4242)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EpubSize != 4242 {
		t.Errorf("EpubSize = %d, want 4242 (the observed size)", got.EpubSize)
	}

	info, err := idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if info[got.EpubPath].Size != got.EpubSize {
		t.Errorf("drift size = %d but book size = %d; they must be one column",
			info[got.EpubPath].Size, got.EpubSize)
	}
}

// Rebuild records skipped directories so AllPathInfo reports every path the
// rebuild accounted for, indexed or not.
func TestRebuildRecordsSkippedPaths(t *testing.T) {
	idx := openTestIndex(t)

	skipped := map[string]drift.PathInfo{
		"Corrupt/Bad Book (7)/book.epub": {Size: 99, EpubMtime: time.Unix(1700000000, 5)},
	}
	if err := idx.Rebuild(bookPaths(newBook(1, "Good")), skipped, 7); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	all, err := idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AllPathInfo has %d paths, want 2 (1 indexed + 1 skipped): %v", len(all), all)
	}
	got, ok := all["Corrupt/Bad Book (7)/book.epub"]
	if !ok {
		t.Fatalf("skipped path missing from AllPathInfo: %v", all)
	}
	if got.Size != 99 || !got.EpubMtime.Equal(time.Unix(1700000000, 5)) {
		t.Errorf("skipped info = %+v, want Size 99 and the recorded mtime", got)
	}

	// A later rebuild must not leave the previous run's skips behind.
	if err := idx.Rebuild(bookPaths(newBook(1, "Good")), nil, 7); err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	all, err = idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if _, ok := all["Corrupt/Bad Book (7)/book.epub"]; ok {
		t.Errorf("stale skipped path survived a rebuild that did not skip it: %v", all)
	}
}

// makeAuthoredBook builds a minimal valid book for insertion. It is the one
// place the book literal lives; newBook and makeTestBook narrow it for the
// tests that don't care about every field.
func makeAuthoredBook(id int64, title string, authors ...model.Author) *book.Book {
	return book.NewBook(
		book.Bib{Title: title, Authors: authors},
		book.Meta{ID: id},
		book.Location{EpubPath: title + "/book.epub"},
	)
}

// newBook is makeAuthoredBook for tests that don't care who wrote it.
func newBook(id int64, title string) *book.Book {
	return makeAuthoredBook(id, title, model.Author{Name: "Alice", SortName: "Alice"})
}

// makeTestBook is makeAuthoredBook for the query and search tests, which filter
// on author name, tag and status but never on sort name. An empty tag leaves
// the book untagged rather than carrying one named "".
func makeTestBook(id int64, title string, authors []string, tag string, status string) *book.Book {
	auths := make([]model.Author, len(authors))
	for i, name := range authors {
		auths[i] = model.Author{Name: name}
	}
	b := makeAuthoredBook(id, title, auths...)
	b.Meta.Status = status
	if tag != "" {
		b.Meta.Tags = []string{tag}
	}
	return b
}

// pendingCount reports how many rows are in pending_ops (white-box access).
func pendingCount(t *testing.T, idx *Index) int {
	t.Helper()
	var n int
	if err := idx.db.QueryRow("SELECT COUNT(*) FROM pending_ops").Scan(&n); err != nil {
		t.Fatalf("count pending_ops: %v", err)
	}
	return n
}

func mustNeedReindex(t *testing.T, idx *Index, want bool) {
	t.Helper()
	got, err := idx.NeedsReindex()
	if err != nil {
		t.Fatalf("NeedsReindex: %v", err)
	}
	if got != want {
		t.Fatalf("NeedsReindex = %v, want %v", got, want)
	}
}

// A completed mutation leaves no pending row and the book queryable.
func TestPutSuccessLeavesClean(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.MarkPending()
	if err := op.Put(newBook(1, "Clean"), drift.PathInfo{}); err != nil {
		t.Fatalf("put: %v", err)
	}

	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
	if _, err := idx.Get(1); err != nil {
		t.Fatalf("get: %v", err)
	}
}

// Put without MarkPending must be rejected — without a pending row the
// preceding store writes have no crash protection.
func TestPutWithoutMarkPendingErrors(t *testing.T) {
	idx := openTestIndex(t)
	op := idx.BeginOp()
	if err := op.Put(newBook(1, "Oops"), drift.PathInfo{}); err == nil {
		t.Fatal("expected error when Put is called without MarkPending")
	}
}

// Delete without MarkPending must be rejected.
func TestDeleteWithoutMarkPendingErrors(t *testing.T) {
	idx := openTestIndex(t)
	op := idx.BeginOp()
	if err := op.Delete(1); err == nil {
		t.Fatal("expected error when Delete is called without MarkPending")
	}
}

// Defect (c): a store write that fails after starting leaves a pending row so
// the next startup reindexes and heals any partial on-disk divergence.
func TestStoreFailureKeepsPending(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.MarkPending()
	// Simulate a store failure after marking pending — Put is never called.

	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops = %d, want 1", n)
	}
	mustNeedReindex(t, idx, true)
}

// Defect (a): a mutation refused before it touches disk (it never calls
// markPending) leaves no row, so it forces no needless reindex.
func TestPreStoreRefusalKeepsNoRow(t *testing.T) {
	idx := openTestIndex(t)

	_ = idx.BeginOp()
	// Never call MarkPending — simulates a refusal before any disk mutation.

	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0 (a pre-disk refusal must not mark pending)", n)
	}
	mustNeedReindex(t, idx, false)
}

// markPending is idempotent: calling it more than once inserts a single row.
func TestMarkPendingIdempotent(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.MarkPending()
	op.MarkPending() // second call must not insert a second row

	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops = %d, want 1 (markPending must insert at most one row)", n)
	}
}

// Defect (b): each operation owns its own pending row, so a concurrent success
// deletes only its own marker and cannot clear a failed peer's row — the exact
// clobber the old single shared dirty flag allowed.
func TestPerOpIndependence(t *testing.T) {
	idx := openTestIndex(t)

	// Op A: MarkPending but never call Put — simulate a failed store write.
	opA := idx.BeginOp()
	opA.MarkPending()

	// Op B: MarkPending and complete successfully.
	opB := idx.BeginOp()
	opB.MarkPending()
	if err := opB.Put(newBook(2, "B"), drift.PathInfo{}); err != nil {
		t.Fatalf("op B: %v", err)
	}

	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops = %d, want 1 (A's row must survive B's commit)", n)
	}
	mustNeedReindex(t, idx, true)
	if _, err := idx.Get(2); err != nil {
		t.Fatalf("op B book missing: %v", err)
	}
}

// Delete clears its own pending row on success.
func TestDeleteSuccessLeavesClean(t *testing.T) {
	idx := openTestIndex(t)

	op1 := idx.BeginOp()
	op1.MarkPending()
	if err := op1.Put(newBook(1, "Gone"), drift.PathInfo{}); err != nil {
		t.Fatalf("put: %v", err)
	}

	op2 := idx.BeginOp()
	op2.MarkPending()
	if err := op2.Delete(1); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
	if _, err := idx.Get(1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("expected book gone (sql.ErrNoRows), got err=%v", err)
	}
}

// A fresh index must force the first reindex even though pending_ops is empty
// (an empty table is the normal clean state and cannot flag a fresh index);
// Rebuild stamps the version, and a clean reopen must not reindex again.
func TestFreshOpenForcesReindex(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")

	idx, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	mustNeedReindex(t, idx, true)

	if err := idx.Rebuild(nil, nil, 0); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	mustNeedReindex(t, idx, false)
	idx.Close()

	idx2, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer idx2.Close()
	mustNeedReindex(t, idx2, false)
}

// A stale schema version forces a reindex regardless of pending_ops.
func TestSchemaBumpForcesReindex(t *testing.T) {
	idx := openTestIndex(t)
	mustNeedReindex(t, idx, false)

	if _, err := idx.db.Exec("PRAGMA user_version=6"); err != nil {
		t.Fatalf("set user_version: %v", err)
	}
	mustNeedReindex(t, idx, true)
}

// Rebuild sweeps leaked pending rows (a crashed op's marker) clean.
func TestRebuildClearsLeakedRows(t *testing.T) {
	idx := openTestIndex(t)

	if _, err := idx.db.Exec("INSERT INTO pending_ops (op_id) VALUES ('leaked')"); err != nil {
		t.Fatalf("inject leaked row: %v", err)
	}
	mustNeedReindex(t, idx, true)

	if err := idx.Rebuild(nil, nil, 0); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
}

// storeInIndex inserts a book into a clean index via Put, failing on error.
func storeInIndex(t *testing.T, idx *Index, b *book.Book) {
	t.Helper()
	storeInIndexSized(t, idx, b, 0)
}

// storeInIndexSized is storeInIndex for tests that care about the epub size the
// index records. The size travels in the observation Put is handed, not on the
// book — books has one epub_size column and it is the stat's.
func storeInIndexSized(t *testing.T, idx *Index, b *book.Book, size int64) {
	t.Helper()
	op := idx.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := op.Put(b, drift.PathInfo{Size: size}); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func TestQueryAllReturnsAllBooks(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Alpha"))
	storeInIndex(t, idx, newBook(2, "Beta"))
	storeInIndex(t, idx, newBook(3, "Gamma"))

	books, err := idx.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(books) != 3 {
		t.Fatalf("len = %d, want 3", len(books))
	}
}

func TestQueryByID(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "One"))
	storeInIndex(t, idx, newBook(2, "Two"))

	got, err := idx.Search(model.Query{IDs: []int64{2}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 2 {
		t.Errorf("ID = %d, want 2", got[0].Meta.ID)
	}
}

func TestQueryByAuthor(t *testing.T) {
	idx := openTestIndex(t)

	bob := book.NewBook(
		book.Bib{Title: "Bob's Book", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "Bob/Bob's Book (1)/book.epub"},
	)
	aliceBook := book.NewBook(
		book.Bib{Title: "Alice's Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "Alice/Alice's Book (2)/book.epub"},
	)

	storeInIndex(t, idx, bob)
	storeInIndex(t, idx, aliceBook)

	got, err := idx.Search(model.Query{Authors: []string{"Bob"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Bob's Book" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Bob's Book")
	}
}

// TestQueryByAuthorSortName pins the two-column author match. ctl's
// rename-author documents that <old> matches display name OR sort name, and it
// relies on this query to find the books to rewrite — if only a.name were
// matched, renaming by sort name would silently rewrite nothing.
func TestQueryByAuthorSortName(t *testing.T) {
	idx := openTestIndex(t)

	asimov := book.NewBook(
		book.Bib{Title: "Foundation", Authors: []model.Author{{Name: "Isaac Asimov", SortName: "Asimov, Isaac"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "Isaac Asimov/Foundation (1)/book.epub"},
	)
	other := book.NewBook(
		book.Bib{Title: "Other", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "Alice/Other (2)/book.epub"},
	)
	storeInIndex(t, idx, asimov)
	storeInIndex(t, idx, other)

	for _, name := range []string{"Asimov, Isaac", "Isaac Asimov"} {
		got, err := idx.Search(model.Query{Authors: []string{name}})
		if err != nil {
			t.Fatalf("Search(%q): %v", name, err)
		}
		if len(got) != 1 || got[0].Title != "Foundation" {
			t.Errorf("Search(%q) = %v, want just Foundation", name, got)
		}
	}
}

func TestQueryByStatus(t *testing.T) {
	idx := openTestIndex(t)

	readBook := book.NewBook(
		book.Bib{Title: "Read Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Status: "read"},
		book.Location{EpubPath: "A/Read Book (1)/book.epub"},
	)
	unreadBook := book.NewBook(
		book.Bib{Title: "Unread Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2, Status: "unread"},
		book.Location{EpubPath: "A/Unread Book (2)/book.epub"},
	)

	storeInIndex(t, idx, readBook)
	storeInIndex(t, idx, unreadBook)

	got, err := idx.Search(model.Query{Status: []string{"read"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 1 {
		t.Errorf("returned book id = %d, want 1", got[0].Meta.ID)
	}
}

func TestQueryByTag(t *testing.T) {
	idx := openTestIndex(t)

	tagged := book.NewBook(
		book.Bib{Title: "Tagged", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"sci-fi"}},
		book.Location{EpubPath: "A/Tagged (1)/book.epub"},
	)
	untagged := book.NewBook(
		book.Bib{Title: "Plain", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Plain (2)/book.epub"},
	)

	storeInIndex(t, idx, tagged)
	storeInIndex(t, idx, untagged)

	got, err := idx.Search(model.Query{Tags: []string{"sci-fi"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Meta.ID != 1 {
		t.Errorf("returned book id = %d, want 1", got[0].Meta.ID)
	}
}

func TestQueryBySeries(t *testing.T) {
	idx := openTestIndex(t)

	seriesBook := book.NewBook(
		book.Bib{
			Title: "Series Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series: &model.SeriesRef{Name: "My Series", Index: "1"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Series Book (1)/book.epub"},
	)
	standalone := book.NewBook(
		book.Bib{Title: "Standalone", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Standalone (2)/book.epub"},
	)

	storeInIndex(t, idx, seriesBook)
	storeInIndex(t, idx, standalone)

	got, err := idx.Search(model.Query{Series: []string{"My Series"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Series Book" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Series Book")
	}
}

func TestQueryMultipleFilters(t *testing.T) {
	idx := openTestIndex(t)

	bobRead := book.NewBook(
		book.Bib{Title: "Bob Read", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 1, Status: "read"},
		book.Location{EpubPath: "B/Bob Read (1)/book.epub"},
	)
	bobUnread := book.NewBook(
		book.Bib{Title: "Bob Unread", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		book.Meta{ID: 2, Status: "unread"},
		book.Location{EpubPath: "B/Bob Unread (2)/book.epub"},
	)
	aliceRead := book.NewBook(
		book.Bib{Title: "Alice Read", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 3, Status: "read"},
		book.Location{EpubPath: "A/Alice Read (3)/book.epub"},
	)

	storeInIndex(t, idx, bobRead)
	storeInIndex(t, idx, bobUnread)
	storeInIndex(t, idx, aliceRead)

	// Filter by Bob AND read.
	got, err := idx.Search(model.Query{Authors: []string{"Bob"}, Status: []string{"read"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Title != "Bob Read" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Bob Read")
	}
}

func TestQueryLimit(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Aardvark"))
	storeInIndex(t, idx, newBook(2, "Beetle"))
	storeInIndex(t, idx, newBook(3, "Cougar"))

	got, err := idx.Search(model.Query{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestQueryRecentOrder(t *testing.T) {
	idx := openTestIndex(t)

	old := book.NewBook(
		book.Bib{Title: "Old", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Old (1)/book.epub"},
	)
	new := book.NewBook(
		book.Bib{Title: "New", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/New (2)/book.epub"},
	)

	storeInIndex(t, idx, old)
	storeInIndex(t, idx, new)

	got, err := idx.Search(model.Query{Order: model.OrderDateAdded})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// OrderDateAdded means date_added DESC. Both books land in the same second
	// here (date_added is RFC3339), so this also pins the id DESC tiebreak.
	if got[0].Meta.ID != 2 {
		t.Errorf("first book id = %d, want 2 (most recently added)", got[0].Meta.ID)
	}
}

// The remaining orders, plus the sort-title tiebreaker that keeps a Limit from
// slicing an arbitrary subset of tied rows.
func TestSearchOrders(t *testing.T) {
	idx := openTestIndex(t)

	mid := newBook(1, "Bravo")
	mid.Meta.Rating = 3
	mid.Pubdate = "2001-01-01"
	top := newBook(2, "Alpha")
	top.Meta.Rating = 5
	top.Pubdate = "2020-01-01"
	// Rating 0 and no pubdate, so these two tie under every order but the
	// title one, exercising the tiebreaker.
	tied := newBook(3, "Charlie")
	untied := newBook(4, "Delta")

	// sort_title is NULL unless the epub carried a file-as refine, so leave it
	// unset on some books: the title ordering has to fall back to the title,
	// not lump them into one NULL tie ordered by id.
	mid.SortTitle = "Bravo"
	top.SortTitle = "Alpha"
	for _, b := range []*book.Book{mid, top, tied, untied} {
		storeInIndex(t, idx, b)
	}

	for _, tc := range []struct {
		name  string
		order model.Order
		want  []int64
	}{
		// 2 "Alpha" and 1 "Bravo" have sort titles; 3 "Charlie" and 4 "Delta"
		// do not and sort by their titles rather than ahead of everything.
		{"sort title", model.OrderSortTitle, []int64{2, 1, 3, 4}},
		{"rating", model.OrderRating, []int64{2, 1, 3, 4}}, // 5, 3, then the 0s by title
		{"pubdate", model.OrderPubdate, []int64{2, 1, 3, 4}},
		{"unknown order falls back to title", model.Order(99), []int64{2, 1, 3, 4}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := idx.Search(model.Query{Order: tc.order})
			if err != nil {
				t.Fatal(err)
			}
			ids := make([]int64, len(got))
			for i, b := range got {
				ids[i] = b.Meta.ID
			}
			if !slices.Equal(ids, tc.want) {
				t.Errorf("ids = %v, want %v", ids, tc.want)
			}
		})
	}
}

func TestQueryEmptyResult(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Only Book"))

	got, err := idx.Search(model.Query{Status: []string{"abandoned"}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestQueryIDNotFound(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Only"))

	got, err := idx.Search(model.Query{IDs: []int64{999}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestQueryLoadsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:       "Identified",
			Authors:     []model.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-3-16-148410-0", "uuid": "abc-def"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Identified (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	books, err := idx.Search(model.Query{IDs: []int64{1}})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(books) != 1 {
		t.Fatalf("len = %d, want 1", len(books))
	}
	if books[0].Identifiers["isbn"] != "978-3-16-148410-0" {
		t.Errorf("Identifier isbn = %q", books[0].Identifiers["isbn"])
	}
	if books[0].Identifiers["uuid"] != "abc-def" {
		t.Errorf("Identifier uuid = %q", books[0].Identifiers["uuid"])
	}
}

func TestQueryBatchLoadsMultipleBooks(t *testing.T) {
	idx := openTestIndex(t)

	// Three books with distinct authors, tags, and identifiers — the batch
	// loading path must hydrate each book correctly from the same three
	// batch queries, not misattribute rows between books.
	books := []*book.Book{
		book.NewBook(
			book.Bib{
				Title:       "Alpha",
				Authors:     []model.Author{{Name: "Alice", SortName: "Alice"}, {Name: "Ariel", SortName: "Ariel"}},
				Identifiers: map[string]string{"isbn": "111"},
			},
			book.Meta{ID: 1, Tags: []string{"fiction", "sci-fi"}},
			book.Location{EpubPath: "A/Alpha (1)/alpha.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:       "Beta",
				Authors:     []model.Author{{Name: "Bob", SortName: "Bob"}},
				Identifiers: map[string]string{"isbn": "222", "doi": "10.1234/beta"},
			},
			book.Meta{ID: 2, Tags: []string{"non-fiction"}},
			book.Location{EpubPath: "B/Beta (2)/beta.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:       "Gamma",
				Authors:     []model.Author{{Name: "Carol", SortName: "Carol"}, {Name: "Charlie", SortName: "Charlie"}, {Name: "Cecil", SortName: "Cecil"}},
				Identifiers: map[string]string{},
			},
			book.Meta{ID: 3},
			book.Location{EpubPath: "C/Gamma (3)/gamma.epub"},
		),
	}
	for _, b := range books {
		storeInIndex(t, idx, b)
	}

	got, err := idx.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}

	for _, b := range got {
		switch b.Meta.ID {
		case int64(1):
			if len(b.Authors) != 2 || b.Authors[0].Name != "Alice" || b.Authors[1].Name != "Ariel" {
				t.Errorf("book 1 authors = %v, want [Alice, Ariel]", b.Authors)
			}
			if !slices.Equal(b.Meta.Tags, []string{"fiction", "sci-fi"}) {
				t.Errorf("book 1 tags = %v, want [fiction, sci-fi]", b.Meta.Tags)
			}
			if b.Identifiers["isbn"] != "111" {
				t.Errorf("book 1 isbn = %q, want 111", b.Identifiers["isbn"])
			}
			if len(b.Identifiers) != 1 {
				t.Errorf("book 1 identifiers = %v, want 1 entry", b.Identifiers)
			}
		case int64(2):
			if len(b.Authors) != 1 || b.Authors[0].Name != "Bob" {
				t.Errorf("book 2 authors = %v, want [Bob]", b.Authors)
			}
			if !slices.Equal(b.Meta.Tags, []string{"non-fiction"}) {
				t.Errorf("book 2 tags = %v, want [non-fiction]", b.Meta.Tags)
			}
			if b.Identifiers["isbn"] != "222" || b.Identifiers["doi"] != "10.1234/beta" {
				t.Errorf("book 2 identifiers = %v", b.Identifiers)
			}
		case int64(3):
			if len(b.Authors) != 3 || b.Authors[0].Name != "Carol" || b.Authors[2].Name != "Cecil" {
				t.Errorf("book 3 authors = %v, want [Carol, Charlie, Cecil]", b.Authors)
			}
			if len(b.Meta.Tags) != 0 {
				t.Errorf("book 3 tags = %v, want empty", b.Meta.Tags)
			}
			if len(b.Identifiers) != 0 {
				t.Errorf("book 3 identifiers = %v, want empty", b.Identifiers)
			}
		default:
			t.Errorf("unexpected book id %d", b.Meta.ID)
		}
	}
}

func TestGetReturnsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:       "Getter",
			Authors:     []model.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-1-234-56789-0"},
		},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/Getter (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Identifiers["isbn"] != "978-1-234-56789-0" {
		t.Errorf("Identifier isbn = %q", got.Identifiers["isbn"])
	}
}

func TestNextID(t *testing.T) {
	idx := openTestIndex(t)
	id1, err := idx.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	id2, err := idx.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if id2 != id1+1 {
		t.Errorf("NextID returned %d then %d, want incrementing by 1", id1, id2)
	}
}

func TestStatsEmptyIndex(t *testing.T) {
	idx := openTestIndex(t)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Books != 0 || s.Authors != 0 || s.Series != 0 || s.Tags != 0 || s.TotalSize != 0 {
		t.Errorf("Stats on empty index = %+v, want all zero", s)
	}
	if !s.LastAdded.IsZero() || !s.LastModified.IsZero() {
		t.Errorf("Stats on empty index = %+v, want zero timestamps", s)
	}
}

func TestStatsAggregates(t *testing.T) {
	idx := openTestIndex(t)

	b1 := book.NewBook(
		book.Bib{
			Title:   "First",
			Authors: []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series:  &model.SeriesRef{Name: "EPIC", Index: "1"},
		},
		book.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		book.Location{EpubPath: "A/First (1)/book.epub"},
	)
	b2 := book.NewBook(
		book.Bib{
			Title:   "Second",
			Authors: []model.Author{{Name: "Alice", SortName: "Alice"}, {Name: "Bob", SortName: "Bob"}},
			Series:  &model.SeriesRef{Name: "EPIC", Index: "2"},
		},
		book.Meta{ID: 2, Tags: []string{"sci-fi"}},
		book.Location{EpubPath: "A/Second (2)/book.epub"},
	)
	storeInIndexSized(t, idx, b1, 100)
	storeInIndexSized(t, idx, b2, 250)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Books != 2 {
		t.Errorf("Books = %d, want 2", s.Books)
	}
	if s.Authors != 2 {
		t.Errorf("Authors = %d, want 2 (Alice, Bob)", s.Authors)
	}
	if s.Series != 1 {
		t.Errorf("Series = %d, want 1 (EPIC)", s.Series)
	}
	if s.Tags != 2 {
		t.Errorf("Tags = %d, want 2 (sci-fi, space)", s.Tags)
	}
	if s.TotalSize != 350 {
		t.Errorf("TotalSize = %d, want 350", s.TotalSize)
	}
	if s.LastAdded.IsZero() || s.LastModified.IsZero() {
		t.Errorf("Stats = %+v, want non-zero timestamps", s)
	}
}

// TestStatsExcludesOrphans exercises the same orphan cleanup TestPutAuthorsWithExistingName
// relies on: replacing a book's tags must not leave the old tag counted in Stats.
func TestStatsExcludesOrphans(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{Title: "Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"stale"}},
		book.Location{EpubPath: "A/Book (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	updated := book.NewBook(
		book.Bib{Title: "Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		book.Meta{ID: 1, Tags: []string{"fresh"}},
		book.Location{EpubPath: "A/Book (1)/book.epub"},
	)
	storeInIndex(t, idx, updated)

	s, err := idx.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if s.Tags != 1 {
		t.Errorf("Tags = %d, want 1 (stale tag should be swept)", s.Tags)
	}
}

func TestSearch(t *testing.T) {
	idx := openTestIndex(t)

	b1 := makeTestBook(1, "Foundation", []string{"Isaac Asimov"}, "sci-fi", book.StatusRead)
	b2 := makeTestBook(2, "Dune", []string{"Frank Herbert"}, "sci-fi", book.StatusUnread)
	b3 := makeTestBook(3, "The Hobbit", []string{"J.R.R. Tolkien"}, "fantasy", book.StatusRead)
	if err := idx.Rebuild(bookPaths(b1, b2, b3), nil, 3); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	t.Run("empty query returns all", func(t *testing.T) {
		books, err := idx.Search(model.Query{})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 3 {
			t.Fatalf("got %d books, want 3", len(books))
		}
	})

	t.Run("single tag", func(t *testing.T) {
		books, err := idx.Search(model.Query{Tags: []string{"sci-fi"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 2 {
			t.Fatalf("got %d books, want 2", len(books))
		}
	})

	t.Run("multiple tags OR", func(t *testing.T) {
		books, err := idx.Search(model.Query{Tags: []string{"sci-fi", "fantasy"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 3 {
			t.Fatalf("got %d books, want 3", len(books))
		}
	})

	t.Run("tag AND status", func(t *testing.T) {
		books, err := idx.Search(model.Query{Tags: []string{"sci-fi"}, Status: []string{"unread"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
		if books[0].Title != "Dune" {
			t.Errorf("got %q, want Dune", books[0].Title)
		}
	})

	t.Run("title substring", func(t *testing.T) {
		books, err := idx.Search(model.Query{Titles: []string{"found"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
		if books[0].Title != "Foundation" {
			t.Errorf("got %q, want Foundation", books[0].Title)
		}
	})

	// ExactTitles turns the substring match into equality, so the same value
	// that found Foundation above finds nothing.
	t.Run("title exact", func(t *testing.T) {
		books, err := idx.Search(model.Query{Titles: []string{"found"}, ExactTitles: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 0 {
			t.Fatalf("got %d books, want 0", len(books))
		}

		books, err = idx.Search(model.Query{Titles: []string{"Foundation"}, ExactTitles: true})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 || books[0].Title != "Foundation" {
			t.Fatalf("got %d books, want Foundation", len(books))
		}
	})

	t.Run("author name", func(t *testing.T) {
		books, err := idx.Search(model.Query{Authors: []string{"Isaac Asimov"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 1 {
			t.Fatalf("got %d books, want 1", len(books))
		}
	})

	t.Run("no match", func(t *testing.T) {
		books, err := idx.Search(model.Query{Tags: []string{"nonexistent"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(books) != 0 {
			t.Fatalf("got %d books, want 0", len(books))
		}
	})
}

func TestSearchIDs(t *testing.T) {
	idx := openTestIndex(t)

	b1 := makeTestBook(1, "A", nil, "", "")
	b2 := makeTestBook(2, "B", nil, "", "")
	if err := idx.Rebuild(bookPaths(b1, b2), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	books, err := idx.Search(model.Query{IDs: []int64{1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 1 {
		t.Fatalf("got %d books, want 1", len(books))
	}
	if books[0].Meta.ID != 1 {
		t.Errorf("got id %d, want 1", books[0].Meta.ID)
	}

	books, err = idx.Search(model.Query{IDs: []int64{1, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(books) != 2 {
		t.Fatalf("got %d books, want 2", len(books))
	}
}

func TestPutAuthorsWithExistingName(t *testing.T) {
	idx := openTestIndex(t)

	authors := []model.Author{{Name: "Alice", SortName: "Smith, Alice"}}
	b1 := book.NewBook(
		book.Bib{Title: "First", Authors: authors},
		book.Meta{ID: 1},
		book.Location{EpubPath: "A/First (1)/book.epub"},
	)
	storeInIndex(t, idx, b1)

	// Second book with same author name — triggers ON CONFLICT upsert.
	b2 := book.NewBook(
		book.Bib{Title: "Second", Authors: authors},
		book.Meta{ID: 2},
		book.Location{EpubPath: "A/Second (2)/book.epub"},
	)
	storeInIndex(t, idx, b2)

	got, err := idx.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestPutBookWithSeriesAndTags(t *testing.T) {
	idx := openTestIndex(t)

	b := book.NewBook(
		book.Bib{
			Title:   "Series Book",
			Authors: []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series:  &model.SeriesRef{Name: "EPIC", Index: "1"},
		},
		book.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		book.Location{EpubPath: "A/Series Book (1)/book.epub"},
	)
	storeInIndex(t, idx, b)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Series == nil || got.Series.Name != "EPIC" {
		t.Errorf("series = %+v", got.Series)
	}
	if len(got.Meta.Tags) != 2 {
		t.Errorf("tags = %v", got.Meta.Tags)
	}
}

// mustMarkPending arms an op, for tests whose subject is the call after it.
func mustMarkPending(t *testing.T, op *Op) {
	t.Helper()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
}

// TestClosedIndexSurfacesErrors checks that every entry point reports the
// failure rather than swallowing it, so a mutation that cannot reach the
// database fails loudly instead of leaving the index quietly wrong.
//
// It asserts only that an error comes back — not which one, and not what state
// survives. Anything stronger belongs with the test that owns that behaviour.
func TestClosedIndexSurfacesErrors(t *testing.T) {
	tests := []struct {
		name string
		// arm runs against the still-open index and returns the call to make
		// once it is closed. Entry points that need a pending op, or a book to
		// act on, set that up here.
		arm func(*testing.T, *Index) func() error
	}{
		{"NextID", func(t *testing.T, idx *Index) func() error {
			return func() error { _, err := idx.NextID(); return err }
		}},
		{"MarkPending", func(t *testing.T, idx *Index) func() error {
			return idx.BeginOp().MarkPending
		}},
		{"Put", func(t *testing.T, idx *Index) func() error {
			op := idx.BeginOp()
			mustMarkPending(t, op)
			return func() error { return op.Put(newBook(1, "T"), drift.PathInfo{}) }
		}},
		{"Delete", func(t *testing.T, idx *Index) func() error {
			storeInIndex(t, idx, newBook(1, "T"))
			op := idx.BeginOp()
			mustMarkPending(t, op)
			return func() error { return op.Delete(1) }
		}},
		{"Rebuild", func(t *testing.T, idx *Index) func() error {
			return func() error { return idx.Rebuild(nil, nil, 0) }
		}},
		{"NeedsReindex", func(t *testing.T, idx *Index) func() error {
			return func() error { _, err := idx.NeedsReindex(); return err }
		}},
		{"dropAllTables", func(t *testing.T, idx *Index) func() error {
			return idx.dropAllTables
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idx := openTestIndex(t)
			call := tc.arm(t, idx)
			idx.Close()

			if err := call(); err == nil {
				t.Errorf("%s on a closed index returned nil, want the failure surfaced", tc.name)
			}
		})
	}
}

// rolledBackTX returns a *sql.Tx that has been rolled back, so any subsequent
// operation on it returns sql.ErrTxDone.
func rolledBackTX(t *testing.T, idx *Index) *sql.Tx {
	t.Helper()
	tx, err := idx.db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	tx.Rollback()
	return tx
}

// TestRolledBackTxSurfacesErrors is TestClosedIndexSurfacesErrors one layer
// down: the helpers that write through a *dbsqlc.Queries must propagate a
// failed statement rather than returning nil and letting the caller commit a
// half-written book. Same caveat — presence of an error is all it asserts.
func TestRolledBackTxSurfacesErrors(t *testing.T) {
	tests := []struct {
		name string
		call func(*Index, *dbsqlc.Queries) error
	}{
		{"finishBook", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.finishBook(q, newBook(1, "Test"))
		}},
		{"upsertAuthors", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.upsertAuthors(q, 1, []model.Author{{Name: "Alice", SortName: "Alice"}})
		}},
		{"upsertTags", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.upsertTags(q, 1, []string{"sci-fi"})
		}},
		{"upsertSeries", func(idx *Index, q *dbsqlc.Queries) error {
			b := newBook(1, "Test")
			b.Series = &model.SeriesRef{Name: "S", Index: "1"}
			return idx.upsertSeries(q, b)
		}},
		{"putBook", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.putBook(q, newBook(1, "Test"), drift.PathInfo{})
		}},
		{"insertBook", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.insertBook(q, newBook(1, "Test"), drift.PathInfo{})
		}},
		{"deleteBook", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.deleteBook(q, 1)
		}},
		{"cleanupOrphans", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.cleanupOrphans(q)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			idx := openTestIndex(t)
			q := dbsqlc.New(rolledBackTX(t, idx))

			if err := tc.call(idx, q); err == nil {
				t.Errorf("%s on a rolled-back tx returned nil, want the failure surfaced", tc.name)
			}
		})
	}
}

// TestRebuildClearsLeakedRowsAndInsertsBooks verifies that Rebuild both
// clears leaked rows and inserts the given books, exercising the full
// Rebuild path through dropAllTables → insertBook → version stamp.
func TestRebuildClearsLeakedRowsAndInsertsBooks(t *testing.T) {
	idx := openTestIndex(t)

	// Inject a leaked pending row and a stale book.
	if _, err := idx.db.Exec("INSERT INTO pending_ops (op_id) VALUES ('leak-1')"); err != nil {
		t.Fatalf("inject pending: %v", err)
	}
	op := idx.BeginOp()
	op.MarkPending()
	if err := op.Put(newBook(1, "Stale"), drift.PathInfo{}); err != nil {
		t.Fatalf("Put stale: %v", err)
	}
	mustNeedReindex(t, idx, true)

	// Rebuild with fresh books.
	fresh := []*book.Book{
		newBook(10, "Fresh A"),
		newBook(20, "Fresh B"),
	}
	if err := idx.Rebuild(bookPaths(fresh...), nil, 20); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	mustNeedReindex(t, idx, false)

	// Only the fresh books should exist.
	all, err := idx.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
}

func TestRebuildWithMultipleBooks(t *testing.T) {
	idx := openTestIndex(t)

	books := []*book.Book{
		book.NewBook(
			book.Bib{Title: "First", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
			book.Meta{ID: 1},
			book.Location{EpubPath: "A/First (1)/book.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:   "Second",
				Authors: []model.Author{{Name: "Bob", SortName: "Bob"}},
				Series:  &model.SeriesRef{Name: "Series A", Index: "2"},
			},
			book.Meta{ID: 2},
			book.Location{EpubPath: "B/Second (2)/book.epub"},
		),
	}

	if err := idx.Rebuild(bookPaths(books...), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	all, err := idx.Search(model.Query{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("len = %d, want 2", len(all))
	}
	if all[0].Meta.ID != 1 || all[1].Meta.ID != 2 {
		t.Errorf("books should be in sort order (by sort_title: %q, %q)", all[0].Title, all[1].Title)
	}

	got, err := idx.Get(2)
	if err != nil {
		t.Fatalf("Get(2): %v", err)
	}
	if got.Series == nil || got.Series.Name != "Series A" {
		t.Errorf("book 2 series = %+v", got.Series)
	}
}

func TestCancelClearsPendingRow(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops after MarkPending = %d, want 1", n)
	}

	op.Cancel()
	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops after Cancel = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
}

func TestCancelWithoutMarkPendingIsNoop(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.Cancel()
	if n := pendingCount(t, idx); n != 0 {
		t.Errorf("pending_ops = %d, want 0", n)
	}
}

func TestOpenFailsCleanlyWhenDBPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir)
	if err == nil {
		t.Fatal("expected error opening a directory as a database")
	}

	// Same check when the path looks like a db file but is a directory.
	dbPath := filepath.Join(dir, "index.db")
	if err := os.Mkdir(dbPath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("expected error opening index at a directory path")
	}
}

// Exists is set equality, not overlap: order is irrelevant, and neither a
// subset nor a superset of a book's authors matches. It is the ingest
// duplicate rule, so a false positive silently refuses a real book and a false
// negative files the same book twice.
func TestExists(t *testing.T) {
	idx := openTestIndex(t)

	solo := makeTestBook(1, "Foundation", []string{"Isaac Asimov"}, "", book.StatusUnread)
	duo := makeTestBook(2, "Good Omens", []string{"Neil Gaiman", "Terry Pratchett"}, "", book.StatusUnread)
	if err := idx.Rebuild(bookPaths(solo, duo), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	hits := []struct {
		title string
		names []string
	}{
		{"Foundation", []string{"Isaac Asimov"}},
		{"Good Omens", []string{"Neil Gaiman", "Terry Pratchett"}},
		{"Good Omens", []string{"Terry Pratchett", "Neil Gaiman"}}, // the order the rule exists for
	}
	for _, tc := range hits {
		ok, err := idx.Exists(tc.title, tc.names)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Errorf("Exists(%q, %q) = false, want true", tc.title, tc.names)
		}
	}

	misses := []struct {
		why   string
		title string
		names []string
	}{
		{"subset of the authors", "Good Omens", []string{"Neil Gaiman"}},
		{"superset of the authors", "Foundation", []string{"Isaac Asimov", "Neil Gaiman"}},
		{"no authors at all", "Foundation", nil},
		{"wrong author", "Foundation", []string{"Frank Herbert"}},
		{"title is exact, not a substring", "found", []string{"Isaac Asimov"}},
		{"right authors, other title", "Dune", []string{"Isaac Asimov"}},
	}
	for _, tc := range misses {
		ok, err := idx.Exists(tc.title, tc.names)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Errorf("Exists(%q, %q) = true, want false (%s)", tc.title, tc.names, tc.why)
		}
	}
}
