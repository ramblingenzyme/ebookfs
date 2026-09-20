package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNextID(t *testing.T) {
	idx := openTestIndex(t)
	id1, err := idx.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	id2, err := idx.NextID()
	if err != nil {
		t.Fatalf("NextID: %v", err)
	}
	if id2 != id1+1 {
		t.Errorf("NextID returned %d then %d, want incrementing by 1", id1, id2)
	}
}

func TestOpenFailsCleanlyWhenDBPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := Open(dir)
	if err == nil {
		t.Fatal("expected error opening a directory as a database")
	}

	// Same check when the path looks like a db file but is a directory.
	dbPath := filepath.Join(dir, "index.db")
	if err := os.Mkdir(dbPath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err = Open(dbPath)
	if err == nil {
		t.Fatal("expected error opening index at a directory path")
	}
}
