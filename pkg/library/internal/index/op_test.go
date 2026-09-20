package index

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
)

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

// Put without MarkPending must be rejected: without a pending row the preceding
// store writes have no crash protection.
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

// Defect (c): a store write that fails after starting leaves a pending row so
// the next startup reindexes and heals any partial on-disk divergence.
func TestStoreFailureKeepsPending(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.MarkPending()
	// Simulate a store failure after marking pending: Put is never called.

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
	// Never call MarkPending, simulating a refusal before any disk mutation.

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
// deletes only its own marker and cannot clear a failed peer's row, the exact
// clobber the old single shared dirty flag allowed.
func TestPerOpIndependence(t *testing.T) {
	idx := openTestIndex(t)

	// Op A: MarkPending but never call Put, simulating a failed store write.
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
