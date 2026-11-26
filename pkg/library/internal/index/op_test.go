package index

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
)

// Each case ends with its pending_ops row count, and the index needs a reindex
// exactly when a row is left.
func TestOpPendingRows(t *testing.T) {
	for _, tc := range []struct {
		name        string
		run         func(*testing.T, *Index)
		wantPending int
	}{
		{"put clears its row", func(t *testing.T, idx *Index) {
			op := mustMarkPending(t, idx)
			if err := op.Put(newBook(1, "Clean"), drift.PathInfo{}); err != nil {
				t.Fatalf("put: %v", err)
			}

			if _, err := idx.Get(1); err != nil {
				t.Fatalf("get: %v", err)
			}
		}, 0},
		{"delete clears its row", func(t *testing.T, idx *Index) {
			storeInIndex(t, idx, newBook(1, "Gone"))
			op := mustMarkPending(t, idx)
			if err := op.Delete(1); err != nil {
				t.Fatalf("delete: %v", err)
			}

			if _, err := idx.Get(1); !errors.Is(err, sql.ErrNoRows) {
				t.Fatalf("expected book gone (sql.ErrNoRows), got err=%v", err)
			}
		}, 0},
		// Without a pending row the preceding store writes have no crash
		// protection.
		{"put after cancel is refused", func(t *testing.T, idx *Index) {
			op := mustMarkPending(t, idx)
			op.Cancel()

			if err := op.Put(newBook(1, "Oops"), drift.PathInfo{}); err == nil {
				t.Fatal("expected error when Put follows Cancel")
			}
		}, 0},
		{"delete after cancel is refused", func(t *testing.T, idx *Index) {
			op := mustMarkPending(t, idx)
			op.Cancel()

			if err := op.Delete(1); err == nil {
				t.Fatal("expected error when Delete follows Cancel")
			}
		}, 0},
		// A single shared dirty flag would let a success clear a failed peer's
		// marker.
		{"a success clears only its own row", func(t *testing.T, idx *Index) {
			mustMarkPending(t, idx)
			storeInIndex(t, idx, newBook(2, "B"))

			if _, err := idx.Get(2); err != nil {
				t.Fatalf("op B book missing: %v", err)
			}
		}, 1},
		{"cancel clears the row", func(t *testing.T, idx *Index) {
			mustMarkPending(t, idx).Cancel()
		}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			idx := openTestIndex(t)
			tc.run(t, idx)

			if n := pendingCount(t, idx); n != tc.wantPending {
				t.Fatalf("pending_ops = %d, want %d", n, tc.wantPending)
			}

			mustNeedReindex(t, idx, tc.wantPending > 0)
		})
	}
}
