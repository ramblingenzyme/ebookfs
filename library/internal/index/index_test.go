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

	if err := idx.Put(newBook(1, "Clean"), func() error { return nil }); err != nil {
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

// Defect (c): a store write that fails after starting leaves a pending row so
// the next startup reindexes and heals any partial on-disk divergence.
func TestStoreFailureKeepsPending(t *testing.T) {
	idx := openTestIndex(t)

	boom := errors.New("store write failed")
	if err := idx.Put(newBook(1, "Doomed"), func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("put err = %v, want %v", err, boom)
	}

	if n := pendingCount(t, idx); n != 1 {
		t.Fatalf("pending_ops = %d, want 1", n)
	}
	mustNeedReindex(t, idx, true)
}

// Defect (b): each operation owns its own pending row, so a concurrent success
// deletes only its own marker and cannot clear a failed peer's row — the exact
// clobber the old single shared dirty flag allowed.
func TestPerOpIndependence(t *testing.T) {
	idx := openTestIndex(t)

	// Op A fails after inserting its row.
	boom := errors.New("boom")
	if err := idx.Put(newBook(1, "A"), func() error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("op A err = %v, want %v", err, boom)
	}
	// Op B succeeds and must delete only its own row.
	if err := idx.Put(newBook(2, "B"), func() error { return nil }); err != nil {
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

	if err := idx.Put(newBook(1, "Gone"), func() error { return nil }); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := idx.Delete(1, func() error { return nil }); err != nil {
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
