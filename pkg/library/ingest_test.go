package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// Index.Exists cannot see a skipped book, so the store check is what refuses
// it.
func TestIngestRejectsUnindexedBookOnDisk(t *testing.T) {
	lib := openTestLibrary(t)
	data := buildTestEpub(t, "Orphaned", "Alice")
	b := ingestTestEpub(t, lib, data)

	// Drop the book from the index only, leaving its files in the tree, the
	// state a skipped directory is in after a rebuild.
	op := lib.index.BeginOp()
	if err := op.MarkPending(); err != nil {
		t.Fatalf("MarkPending: %v", err)
	}
	if err := op.Delete(b.ID()); err != nil {
		t.Fatalf("index delete: %v", err)
	}

	if _, err := tryIngest(t, lib, data); !errors.Is(err, ErrDuplicateOnDisk) {
		t.Fatalf("re-ingest err = %v, want ErrDuplicateOnDisk", err)
	}
}

func TestCreateIngestReadOnlyDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	lib := openTestLibrary(t)

	if err := os.Chmod(lib.inboxTemp, 0444); err != nil {
		t.Skip("cannot chmod inbox temp:", err)
	}
	t.Cleanup(func() { os.Chmod(lib.inboxTemp, 0755) })

	_, err := lib.CreateIngest()
	if err == nil {
		t.Error("expected error when inbox temp is read-only")
	}
}

// An ingest that fails after the store has started writing removes the
// directory it built and clears its pending row, so the next start neither
// rebuilds nor finds a half-built book. A directory occupying meta.toml's name
// fails the sidecar write after the epub is in place.
func TestFailedIngestCleansUp(t *testing.T) {
	lib := openTestLibrary(t)
	loc := lib.store.Layout([]Author{{Name: "Alice"}}, "Doomed", 1)
	dir := lib.store.AbsPath(loc.Dir())
	if err := os.MkdirAll(filepath.Join(dir, "meta.toml"), 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := tryIngest(t, lib, buildTestEpub(t, "Doomed", "Alice")); err == nil {
		t.Fatal("Ingest succeeded with meta.toml's name taken")
	}

	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("book directory left behind (stat err = %v)", err)
	}
	needs, err := lib.index.NeedsReindex()
	if err != nil {
		t.Fatalf("NeedsReindex: %v", err)
	}
	if needs {
		t.Error("the failed ingest left its pending row, so the next start rebuilds")
	}
	if got, err := lib.Search(Query{}); err != nil || len(got) != 0 {
		t.Errorf("Search = %v (err %v), want no books", got, err)
	}
}
