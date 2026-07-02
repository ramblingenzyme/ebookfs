package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCleanInboxTempRemovesStaleEpub(t *testing.T) {
	dir := t.TempDir()

	stale := filepath.Join(dir, "123456789.epub")
	if err := os.WriteFile(stale, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale epub %q should have been removed", stale)
	}
}

func TestCleanInboxTempLeavesNonEpub(t *testing.T) {
	dir := t.TempDir()

	kept := filepath.Join(dir, "readme.txt")
	if err := os.WriteFile(kept, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp: %v", err)
	}

	if _, err := os.Stat(kept); os.IsNotExist(err) {
		t.Errorf("non-epub file %q should not have been removed", kept)
	}
}

func TestCleanInboxTempLeavesDirectories(t *testing.T) {
	dir := t.TempDir()

	subdir := filepath.Join(dir, "subdir")
	if err := os.Mkdir(subdir, 0755); err != nil {
		t.Fatal(err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp: %v", err)
	}

	if _, err := os.Stat(subdir); os.IsNotExist(err) {
		t.Errorf("subdirectory %q should not have been removed", subdir)
	}
}

func TestCleanInboxTempEmptyDir(t *testing.T) {
	dir := t.TempDir()

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp on empty dir: %v", err)
	}
}

func TestCleanInboxTempNonexistentDir(t *testing.T) {
	err := cleanInboxTemp("/nonexistent-path-ebookfs-test")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestCleanInboxTempRemovesMultiple(t *testing.T) {
	dir := t.TempDir()

	files := []string{"a.epub", "b.epub", "c.epub"}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	// Also add a non-epub to verify it survives
	if err := os.WriteFile(filepath.Join(dir, "keep.me"), []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := cleanInboxTemp(dir); err != nil {
		t.Fatalf("cleanInboxTemp: %v", err)
	}

	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != "keep.me" {
		t.Errorf("expected only 'keep.me' to remain, got %v", entryNames(entries))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}
