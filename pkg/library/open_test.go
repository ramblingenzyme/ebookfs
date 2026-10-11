package library

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

// Only regular files ending .epub go. A directory with that suffix stays, so
// the type check is what keeps it.
func TestCleanInboxTemp(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"123.epub", "456.epub", "readme.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "nested.epub"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := entryNames(entries), []string{"nested.epub", "readme.txt"}; !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}

	if err := cleanInboxTemp(t.TempDir()); err != nil {
		t.Errorf("cleanInboxTemp on an empty directory: %v", err)
	}
	if err := cleanInboxTemp(filepath.Join(dir, "missing")); err == nil {
		t.Error("cleanInboxTemp on a missing directory: want an error")
	}
}

func TestCheckSameFilesystemMissingTarget(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "nonexistent")

	err := checkSameFilesystem(dir, missing)
	if err == nil {
		t.Fatal("expected error when target directory doesn't exist")
	}
}
