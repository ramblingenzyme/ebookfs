package index

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"

	"github.com/ramblingenzyme/ebookfs/library/internal/drift"
)

// TestPathInfoRoundTrip pins the nanosecond encoding: every other test passes a
// zero drift.PathInfo, so without this a broken encode/decode would surface only as
// drift detection quietly rebuilding on every startup.
//
// Both write paths are covered because each maps the observation onto its own
// query params — Put through UpsertBook, Rebuild through InsertBook — so an
// encoding fixed in one can stay broken in the other.
func TestPathInfoRoundTrip(t *testing.T) {
	writers := map[string]func(*testing.T, *Index, *book.Book, drift.PathInfo){
		"put": func(t *testing.T, idx *Index, b *book.Book, pi drift.PathInfo) {
			op := idx.BeginOp()
			if err := op.MarkPending(); err != nil {
				t.Fatalf("MarkPending: %v", err)
			}
			if err := op.Put(b, pi); err != nil {
				t.Fatalf("Put: %v", err)
			}
		},
		"rebuild": func(t *testing.T, idx *Index, b *book.Book, pi drift.PathInfo) {
			if err := idx.Rebuild([]BookPath{{Book: b, Info: pi}}, nil, b.Meta.ID); err != nil {
				t.Fatalf("Rebuild: %v", err)
			}
		},
	}
	tests := []struct {
		name string
		want drift.PathInfo
	}{
		// A whole second (zero nanoseconds) alongside sub-millisecond precision
		// that a second-granularity format would silently truncate.
		{"recorded", drift.PathInfo{
			Size:      4242,
			EpubMtime: time.Unix(1700000000, 0),
			MetaSize:  17,
			MetaMtime: time.Unix(1700000000, 123456789),
		}},
		// Never observed: stores as 0 and must decode back to the zero time, not
		// to the Unix epoch, which would read as a real (and wrong) timestamp.
		{"zero", drift.PathInfo{}},
	}
	for writer, write := range writers {
		for _, tc := range tests {
			t.Run(writer+"/"+tc.name, func(t *testing.T) {
				idx := openTestIndex(t)
				write(t, idx, newBook(1, tc.name), tc.want)

				all, err := idx.AllPathInfo()
				if err != nil {
					t.Fatalf("AllPathInfo: %v", err)
				}
				got, ok := all[tc.name+"/book.epub"]
				if !ok {
					t.Fatalf("AllPathInfo missing %q, got %v", tc.name, all)
				}
				if !got.Equal(tc.want) {
					t.Errorf("drift.PathInfo = %+v, want %+v", got, tc.want)
				}
			})
		}
	}
}

// TestEpubSizeComesFromObservation pins the collapse of the epub's two size
// columns into one: books.epub_size is written from the observation handed to
// Put, and a size set on the book itself is ignored. The book's copy is what 9P
// reports as the file's length and what export sizing uses, while the drift
// check compares the stat's — so a second column here means those two can
// silently disagree, which is what this replaced.
func TestEpubSizeComesFromObservation(t *testing.T) {
	idx := openTestIndex(t)

	b := newBook(1, "Sized")
	b.EpubSize = 7 // discarded: the observation below is what lands in the row
	storeInIndexSized(t, idx, b, 4242)

	got, err := idx.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.EpubSize != 4242 {
		t.Errorf("EpubSize = %d, want 4242 (the observed size)", got.EpubSize)
	}

	info, err := idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if info[got.EpubPath].Size != got.EpubSize {
		t.Errorf("drift size = %d but book size = %d; they must be one column",
			info[got.EpubPath].Size, got.EpubSize)
	}
}

// Rebuild records skipped directories so AllPathInfo reports every path the
// rebuild accounted for, indexed or not.
func TestRebuildRecordsSkippedPaths(t *testing.T) {
	idx := openTestIndex(t)

	skipped := map[string]drift.PathInfo{
		"Corrupt/Bad Book (7)/book.epub": {Size: 99, EpubMtime: time.Unix(1700000000, 5)},
	}
	if err := idx.Rebuild(bookPaths(newBook(1, "Good")), skipped, 7); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	all, err := idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("AllPathInfo has %d paths, want 2 (1 indexed + 1 skipped): %v", len(all), all)
	}
	got, ok := all["Corrupt/Bad Book (7)/book.epub"]
	if !ok {
		t.Fatalf("skipped path missing from AllPathInfo: %v", all)
	}
	if got.Size != 99 || !got.EpubMtime.Equal(time.Unix(1700000000, 5)) {
		t.Errorf("skipped info = %+v, want Size 99 and the recorded mtime", got)
	}

	// A later rebuild must not leave the previous run's skips behind.
	if err := idx.Rebuild(bookPaths(newBook(1, "Good")), nil, 7); err != nil {
		t.Fatalf("second Rebuild: %v", err)
	}
	all, err = idx.AllPathInfo()
	if err != nil {
		t.Fatalf("AllPathInfo: %v", err)
	}
	if _, ok := all["Corrupt/Bad Book (7)/book.epub"]; ok {
		t.Errorf("stale skipped path survived a rebuild that did not skip it: %v", all)
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

	if err := idx.Rebuild(nil, nil, 0); err != nil {
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

	if err := idx.Rebuild(nil, nil, 0); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if n := pendingCount(t, idx); n != 0 {
		t.Fatalf("pending_ops = %d, want 0", n)
	}
	mustNeedReindex(t, idx, false)
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
	if err := op.Put(newBook(1, "Stale"), drift.PathInfo{}); err != nil {
		t.Fatalf("Put stale: %v", err)
	}
	mustNeedReindex(t, idx, true)

	// Rebuild with fresh books.
	fresh := []*book.Book{
		newBook(10, "Fresh A"),
		newBook(20, "Fresh B"),
	}
	if err := idx.Rebuild(bookPaths(fresh...), nil, 20); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	mustNeedReindex(t, idx, false)

	// Only the fresh books should exist.
	all, err := idx.Search(Query{})
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

	books := []*book.Book{
		book.NewBook(
			book.Bib{Title: "First", Authors: []book.Author{{Name: "Alice", SortName: "Alice"}}},
			book.Meta{ID: 1},
			book.Location{EpubPath: "A/First (1)/book.epub"},
		),
		book.NewBook(
			book.Bib{
				Title:   "Second",
				Authors: []book.Author{{Name: "Bob", SortName: "Bob"}},
				Series:  &book.SeriesRef{Name: "Series A", Index: "2"},
			},
			book.Meta{ID: 2},
			book.Location{EpubPath: "B/Second (2)/book.epub"},
		),
	}

	if err := idx.Rebuild(bookPaths(books...), nil, 2); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}

	all, err := idx.Search(Query{})
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
