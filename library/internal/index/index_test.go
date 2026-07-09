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
