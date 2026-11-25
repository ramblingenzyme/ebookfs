// Error propagation, swept across the package rather than per function. A
// closed database is injected and every entry point that should surface it is
// driven in one table.
//
// The rule is that no write path swallows a database error.

package index

import (
	"testing"

	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
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
			return func() error { _, err := idx.MarkPending(); return err }
		}},
		{"Put", func(t *testing.T, idx *Index) func() error {
			op := mustMarkPending(t, idx)
			return func() error { return op.Put(newBook(1, "T"), drift.PathInfo{}) }
		}},
		{"Delete", func(t *testing.T, idx *Index) func() error {
			storeInIndex(t, idx, newBook(1, "T"))
			op := mustMarkPending(t, idx)
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
