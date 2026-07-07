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
