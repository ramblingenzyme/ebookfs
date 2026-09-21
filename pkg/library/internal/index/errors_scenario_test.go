// Error propagation, swept across the package rather than per function. Two
// failures are injected and every path that should surface them is driven in
// one table each: a closed database (NextID, MarkPending, Put, Delete, Rebuild,
// NeedsReindex, dropAllTables) and a rolled-back transaction (insertBook,
// putBook, finishBook, the three upserts, deleteBook, cleanupOrphans).
//
// The rule is that no write path swallows a database error.

package index

import (
	"database/sql"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index/dbsqlc"
)

// Every entry point reports the failure rather than swallowing it, so a
// mutation that cannot reach the database fails loudly instead of leaving the
// index quietly wrong.
//
// It asserts only that an error comes back, not which one and not what state
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

// The helpers that write through a *dbsqlc.Queries: a failed statement must
// propagate rather than return nil and let the caller commit a half-written
// book.
func TestRolledBackTxSurfacesErrors(t *testing.T) {
	tests := []struct {
		name string
		call func(*Index, *dbsqlc.Queries) error
	}{
		{"finishBook", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.finishBook(q, newBook(1, "Test"))
		}},
		{"upsertAuthors", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.upsertAuthors(q, 1, []book.Author{{Name: "Alice", SortName: "Alice"}})
		}},
		{"upsertTags", func(idx *Index, q *dbsqlc.Queries) error {
			return idx.upsertTags(q, 1, []string{"sci-fi"})
		}},
		{"upsertSeries", func(idx *Index, q *dbsqlc.Queries) error {
			b := newBook(1, "Test")
			b.Series = &book.SeriesRef{Name: "S", Index: "1"}
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
