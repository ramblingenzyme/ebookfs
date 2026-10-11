package index

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
)

// A completed mutation leaves the book queryable.
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

func TestDeleteWithoutMarkPendingErrors(t *testing.T) {
	idx := openTestIndex(t)
	op := idx.BeginOp()
	if err := op.Delete(1); err == nil {
		t.Fatal("expected error when Delete is called without MarkPending")
	}
}

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

// More than one MarkPending inserts a single row.
func TestMarkPendingIdempotent(t *testing.T) {
	idx := openTestIndex(t)

	op := idx.BeginOp()
	op.MarkPending()
	op.MarkPending() // second call must not insert a second row

	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops = %d, want 1 (markPending must insert at most one row)", n)
	}
}

// Each operation owns its own pending row, so a concurrent success deletes
// only its own marker. A single shared dirty flag would let it clear a failed
// peer's.
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
