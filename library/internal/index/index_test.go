package index

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/library/model"
)

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
	if err := idx.Rebuild(nil, 0); err != nil {
		t.Fatalf("baseline rebuild: %v", err)
	}
	return idx
}

// newBook builds a minimal valid book for insertion.
func newBook(id int64, title string) *model.Book {
	return model.NewBook(
		model.Bib{Title: title, Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: id},
		model.Location{LibraryPath: title, EpubFilename: "book.epub"},
	)
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
	if err := op.Put(newBook(1, "Clean")); err != nil {
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
	if err := op.Put(newBook(1, "Oops")); err == nil {
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
	if err := opB.Put(newBook(2, "B")); err != nil {
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
	if err := op1.Put(newBook(1, "Gone")); err != nil {
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

	if err := idx.Rebuild(nil, 0); err != nil {
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

	if err := idx.Rebuild(nil, 0); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
}

// storeInIndex inserts a book into a clean index via Put, failing on error.
func storeInIndex(t *testing.T, idx *Index, b *model.Book) {
	t.Helper()
	op := idx.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := op.Put(b); err != nil {
		t.Fatalf("Put: %v", err)
	}
}

func TestQueryAllReturnsAllBooks(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Alpha"))
	storeInIndex(t, idx, newBook(2, "Beta"))
	storeInIndex(t, idx, newBook(3, "Gamma"))

	books, err := idx.Query(model.Filter{})
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

	got, err := idx.Query(model.Filter{ID: 2})
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

	bob := model.NewBook(
		model.Bib{Title: "Bob's Book", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "Bob/Bob's Book (1)", EpubFilename: "book.epub"},
	)
	aliceBook := model.NewBook(
		model.Bib{Title: "Alice's Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 2},
		model.Location{LibraryPath: "Alice/Alice's Book (2)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, bob)
	storeInIndex(t, idx, aliceBook)

	got, err := idx.Query(model.Filter{Author: "Bob"})
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

func TestQueryByStatus(t *testing.T) {
	idx := openTestIndex(t)

	readBook := model.NewBook(
		model.Bib{Title: "Read Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 1, Status: "read"},
		model.Location{LibraryPath: "A/Read Book (1)", EpubFilename: "book.epub"},
	)
	unreadBook := model.NewBook(
		model.Bib{Title: "Unread Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 2, Status: "unread"},
		model.Location{LibraryPath: "A/Unread Book (2)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, readBook)
	storeInIndex(t, idx, unreadBook)

	got, err := idx.Query(model.Filter{Status: "read"})
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

	tagged := model.NewBook(
		model.Bib{Title: "Tagged", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 1, Tags: []string{"sci-fi"}},
		model.Location{LibraryPath: "A/Tagged (1)", EpubFilename: "book.epub"},
	)
	untagged := model.NewBook(
		model.Bib{Title: "Plain", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 2},
		model.Location{LibraryPath: "A/Plain (2)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, tagged)
	storeInIndex(t, idx, untagged)

	got, err := idx.Query(model.Filter{Tag: "sci-fi"})
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

	seriesBook := model.NewBook(
		model.Bib{
			Title: "Series Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series: &model.SeriesRef{Name: "My Series", Index: 1},
		},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "A/Series Book (1)", EpubFilename: "book.epub"},
	)
	standalone := model.NewBook(
		model.Bib{Title: "Standalone", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 2},
		model.Location{LibraryPath: "A/Standalone (2)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, seriesBook)
	storeInIndex(t, idx, standalone)

	got, err := idx.Query(model.Filter{Series: "My Series"})
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

	bobRead := model.NewBook(
		model.Bib{Title: "Bob Read", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		model.Meta{ID: 1, Status: "read"},
		model.Location{LibraryPath: "B/Bob Read (1)", EpubFilename: "book.epub"},
	)
	bobUnread := model.NewBook(
		model.Bib{Title: "Bob Unread", Authors: []model.Author{{Name: "Bob", SortName: "Bob"}}},
		model.Meta{ID: 2, Status: "unread"},
		model.Location{LibraryPath: "B/Bob Unread (2)", EpubFilename: "book.epub"},
	)
	aliceRead := model.NewBook(
		model.Bib{Title: "Alice Read", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 3, Status: "read"},
		model.Location{LibraryPath: "A/Alice Read (3)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, bobRead)
	storeInIndex(t, idx, bobUnread)
	storeInIndex(t, idx, aliceRead)

	// Filter by Bob AND read.
	got, err := idx.Query(model.Filter{Author: "Bob", Status: "read"})
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

	got, err := idx.Query(model.Filter{Limit: 2})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestQueryRecentOrder(t *testing.T) {
	idx := openTestIndex(t)

	old := model.NewBook(
		model.Bib{Title: "Old", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "A/Old (1)", EpubFilename: "book.epub"},
	)
	new := model.NewBook(
		model.Bib{Title: "New", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 2},
		model.Location{LibraryPath: "A/New (2)", EpubFilename: "book.epub"},
	)

	storeInIndex(t, idx, old)
	storeInIndex(t, idx, new)

	got, err := idx.Query(model.Filter{Recent: true})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Recent means date_added DESC — the most recently inserted should be first.
	if got[0].Meta.ID != 2 {
		t.Errorf("first book id = %d, want 2 (most recently added)", got[0].Meta.ID)
	}
}

func TestQueryEmptyResult(t *testing.T) {
	idx := openTestIndex(t)
	storeInIndex(t, idx, newBook(1, "Only Book"))

	got, err := idx.Query(model.Filter{Status: "abandoned"})
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

	got, err := idx.Query(model.Filter{ID: 999})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("len = %d, want 0", len(got))
	}
}

func TestQueryLoadsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := model.NewBook(
		model.Bib{
			Title:       "Identified",
			Authors:     []model.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-3-16-148410-0", "uuid": "abc-def"},
		},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "A/Identified (1)", EpubFilename: "book.epub"},
	)
	storeInIndex(t, idx, b)

	books, err := idx.Query(model.Filter{ID: 1})
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

func TestGetReturnsIdentifiers(t *testing.T) {
	idx := openTestIndex(t)

	b := model.NewBook(
		model.Bib{
			Title:       "Getter",
			Authors:     []model.Author{{Name: "Alice", SortName: "Alice"}},
			Identifiers: map[string]string{"isbn": "978-1-234-56789-0"},
		},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "A/Getter (1)", EpubFilename: "book.epub"},
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

func TestListAuthorsPanics(t *testing.T) {
	idx := openTestIndex(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from ListAuthors")
		}
	}()
	idx.ListAuthors()
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

	b1 := model.NewBook(
		model.Bib{
			Title:    "First",
			Authors:  []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series:   &model.SeriesRef{Name: "EPIC", Index: 1},
			EpubSize: 100,
		},
		model.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		model.Location{LibraryPath: "A/First (1)", EpubFilename: "book.epub"},
	)
	b2 := model.NewBook(
		model.Bib{
			Title:    "Second",
			Authors:  []model.Author{{Name: "Alice", SortName: "Alice"}, {Name: "Bob", SortName: "Bob"}},
			Series:   &model.SeriesRef{Name: "EPIC", Index: 2},
			EpubSize: 250,
		},
		model.Meta{ID: 2, Tags: []string{"sci-fi"}},
		model.Location{LibraryPath: "A/Second (2)", EpubFilename: "book.epub"},
	)
	storeInIndex(t, idx, b1)
	storeInIndex(t, idx, b2)

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

	b := model.NewBook(
		model.Bib{Title: "Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 1, Tags: []string{"stale"}},
		model.Location{LibraryPath: "A/Book (1)", EpubFilename: "book.epub"},
	)
	storeInIndex(t, idx, b)

	updated := model.NewBook(
		model.Bib{Title: "Book", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
		model.Meta{ID: 1, Tags: []string{"fresh"}},
		model.Location{LibraryPath: "A/Book (1)", EpubFilename: "book.epub"},
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

func TestSearchPanics(t *testing.T) {
	idx := openTestIndex(t)
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic from Search")
		}
	}()
	idx.Search("query")
}

func TestPutAuthorsWithExistingName(t *testing.T) {
	idx := openTestIndex(t)

	authors := []model.Author{{Name: "Alice", SortName: "Smith, Alice"}}
	b1 := model.NewBook(
		model.Bib{Title: "First", Authors: authors},
		model.Meta{ID: 1},
		model.Location{LibraryPath: "A/First (1)", EpubFilename: "book.epub"},
	)
	storeInIndex(t, idx, b1)

	// Second book with same author name — triggers ON CONFLICT upsert.
	b2 := model.NewBook(
		model.Bib{Title: "Second", Authors: authors},
		model.Meta{ID: 2},
		model.Location{LibraryPath: "A/Second (2)", EpubFilename: "book.epub"},
	)
	storeInIndex(t, idx, b2)

	got, err := idx.Query(model.Filter{})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
}

func TestPutBookWithSeriesAndTags(t *testing.T) {
	idx := openTestIndex(t)

	b := model.NewBook(
		model.Bib{
			Title:   "Series Book",
			Authors: []model.Author{{Name: "Alice", SortName: "Alice"}},
			Series:  &model.SeriesRef{Name: "EPIC", Index: 1},
		},
		model.Meta{ID: 1, Tags: []string{"sci-fi", "space"}},
		model.Location{LibraryPath: "A/Series Book (1)", EpubFilename: "book.epub"},
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

func TestNextIDClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	idx.Close()
	_, err := idx.NextID()
	if err == nil {
		t.Fatal("expected error from NextID after db closed")
	}
}

func TestMarkPendingClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	op := idx.BeginOp()
	idx.Close()
	err := op.MarkPending()
	if err == nil {
		t.Fatal("expected error from MarkPending after db closed")
	}
}

func TestPutClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	op := idx.BeginOp()
	op.MarkPending()
	idx.Close()
	err := op.Put(newBook(1, "T"))
	if err == nil {
		t.Fatal("expected error from Put after db closed")
	}
}

func TestDeleteClosedDB(t *testing.T) {
	idx := openTestIndex(t)

	op1 := idx.BeginOp()
	op1.MarkPending()
	if err := op1.Put(newBook(1, "T")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	op2 := idx.BeginOp()
	op2.MarkPending()
	idx.Close()
	err := op2.Delete(1)
	if err == nil {
		t.Fatal("expected error from Delete after db closed")
	}
}

func TestRebuildClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	idx.Close()
	err := idx.Rebuild(nil, 0)
	if err == nil {
		t.Fatal("expected error from Rebuild after db closed")
	}
}

func TestNeedsReindexClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	idx.Close()
	_, err := idx.NeedsReindex()
	if err == nil {
		t.Fatal("expected error from NeedsReindex after db closed")
	}
}

// rolledBackTx returns a *sql.Tx that has been rolled back, so any subsequent
// operation on it returns sql.ErrTxDone. Tests tx-level error branches.
func rolledBackTX(t *testing.T, idx *Index) *sql.Tx {
	t.Helper()
	tx, err := idx.db.Begin()
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	tx.Rollback()
	return tx
}

func TestFinishBookRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := finishBook(tx, newBook(1, "Test"))
	if err == nil {
		t.Fatal("expected error from finishBook on rolled-back tx")
	}
}

func TestUpsertAuthorsRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := upsertAuthors(tx, 1, []model.Author{{Name: "Alice", SortName: "Alice"}})
	if err == nil {
		t.Fatal("expected error from upsertAuthors on rolled-back tx")
	}
}

func TestUpsertTagsRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := upsertTags(tx, 1, []string{"sci-fi"})
	if err == nil {
		t.Fatal("expected error from upsertTags on rolled-back tx")
	}
}

func TestUpsertSeriesRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	b := newBook(1, "Test")
	b.Series = &model.SeriesRef{Name: "S", Index: 1}
	err := upsertSeries(tx, b)
	if err == nil {
		t.Fatal("expected error from upsertSeries on rolled-back tx")
	}
}

func TestPutBookRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := putBook(tx, newBook(1, "Test"))
	if err == nil {
		t.Fatal("expected error from putBook on rolled-back tx")
	}
}

func TestInsertBookRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := insertBook(tx, newBook(1, "Test"))
	if err == nil {
		t.Fatal("expected error from insertBook on rolled-back tx")
	}
}

func TestDeleteBookRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := deleteBook(tx, 1)
	if err == nil {
		t.Fatal("expected error from deleteBook on rolled-back tx")
	}
}

func TestCleanupOrphansRolledBackTx(t *testing.T) {
	idx := openTestIndex(t)
	tx := rolledBackTX(t, idx)
	err := cleanupOrphans(tx)
	if err == nil {
		t.Fatal("expected error from cleanupOrphans on rolled-back tx")
	}
}

func TestDropAllTablesClosedDB(t *testing.T) {
	idx := openTestIndex(t)
	idx.Close()
	err := idx.dropAllTables()
	if err == nil {
		t.Fatal("expected error from dropAllTables after db closed")
	}
}

func TestOpenDirectoryPath(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir)
	if err == nil {
		t.Fatal("expected error opening a directory as a database")
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
	if err := op.Put(newBook(1, "Stale")); err != nil {
		t.Fatalf("Put stale: %v", err)
	}
	mustNeedReindex(t, idx, true)

	// Rebuild with fresh books.
	fresh := []*model.Book{
		newBook(10, "Fresh A"),
		newBook(20, "Fresh B"),
	}
	if err := idx.Rebuild(fresh, 20); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	mustNeedReindex(t, idx, false)

	// Only the fresh books should exist.
	all, err := idx.Query(model.Filter{})
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

	books := []*model.Book{
		model.NewBook(
			model.Bib{Title: "First", Authors: []model.Author{{Name: "Alice", SortName: "Alice"}}},
			model.Meta{ID: 1},
			model.Location{LibraryPath: "A/First (1)", EpubFilename: "book.epub"},
		),
		model.NewBook(
			model.Bib{
				Title:   "Second",
				Authors: []model.Author{{Name: "Bob", SortName: "Bob"}},
				Series:  &model.SeriesRef{Name: "Series A", Index: 2},
			},
			model.Meta{ID: 2},
			model.Location{LibraryPath: "B/Second (2)", EpubFilename: "book.epub"},
		),
	}

	if err := idx.Rebuild(books, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	all, err := idx.Query(model.Filter{})
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
