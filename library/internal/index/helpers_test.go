package index

import (
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
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
	// A fresh index is intentionally "dirty" (no version stamp), so rebuild an
	// empty one to reach the clean baseline the mutation tests want.
	if err := idx.Rebuild(nil, nil, 0); err != nil {
		t.Fatalf("baseline rebuild: %v", err)
	}
	return idx
}

// makeAuthoredBook builds a minimal valid book for insertion. It is the one
// place the book literal lives; newBook and makeTestBook narrow it for the
// tests that don't care about every field.
func makeAuthoredBook(id int64, title string, authors ...book.Author) *book.Book {
	return book.NewBook(
		book.Bib{Title: title, Authors: authors},
		book.Meta{ID: id},
		book.Location{EpubPath: title + "/book.epub"},
	)
}

// newBook is makeAuthoredBook for tests that don't care who wrote it.
func newBook(id int64, title string) *book.Book {
	return makeAuthoredBook(id, title, book.Author{Name: "Alice", SortName: "Alice"})
}

// makeTestBook is makeAuthoredBook for the query and search tests, which filter
// on author name, tag and status but never on sort name. An empty tag leaves
// the book untagged rather than carrying one named "".
func makeTestBook(id int64, title string, authors []string, tag string, status string) *book.Book {
	auths := make([]book.Author, len(authors))
	for i, name := range authors {
		auths[i] = book.Author{Name: name}
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

// storeInIndex inserts a book into a clean index via Put, failing on error.
func storeInIndex(t *testing.T, idx *Index, b *book.Book) {
	t.Helper()
	storeInIndexSized(t, idx, b, 0)
}

// storeInIndexSized is storeInIndex for tests that care about the epub size the
// index records. The size travels in the observation Put is handed, not on the
// book: books has one epub_size column and it is the stat's.
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

// mustMarkPending arms an op, for tests whose subject is the call after it.
func mustMarkPending(t *testing.T, op *Op) {
	t.Helper()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
}
