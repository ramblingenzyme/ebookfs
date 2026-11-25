package store

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	epubfile "github.com/ramblingenzyme/ebookfs/pkg/epub"
)

// Every temp name a writer in the tree mints is one the sweep removes.
func TestRemoveStaleTempsMatchesEveryWriter(t *testing.T) {
	s, root := newStore(t)
	dir := filepath.Join(root, "Author", "Title (1)")

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	var stale []string

	for _, pattern := range []string{tempPattern(metaFilename), tempPattern(lastIDName), epubfile.SaveTempPattern} {
		f, err := os.CreateTemp(dir, pattern)
		if err != nil {
			t.Fatal(err)
		}

		f.Close()
		stale = append(stale, f.Name())
	}
	// Written by Save before it had a pattern of its own.
	legacy := filepath.Join(dir, ".ebookfs-12345.epub.tmp")
	stale = append(stale, legacy)

	kept := []string{
		filepath.Join(dir, "book.epub"),
		filepath.Join(dir, metaFilename),
		filepath.Join(dir, "other.tmp"),
		filepath.Join(dir, ".syncthing.book.epub.tmp"),
	}
	for _, path := range append(kept, legacy) {
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	if err := s.RemoveStaleTemps(); err != nil {
		t.Fatalf("RemoveStaleTemps: %v", err)
	}

	for _, path := range stale {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived the sweep", filepath.Base(path))
		}
	}

	for _, path := range kept {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("%s was removed: %v", filepath.Base(path), err)
		}
	}
}

// One unreadable book must not stop the sweep of the books after it.
func TestRemoveStaleTempsPastUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}

	s, root := newStore(t)
	unreadable := filepath.Join(root, "A")
	later := filepath.Join(root, "Z")

	for _, dir := range []string{unreadable, later} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	stale := filepath.Join(later, ".ebookfs-1.epub.tmp")
	if err := os.WriteFile(stale, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = os.Chmod(unreadable, 0o755) })

	if err := s.RemoveStaleTemps(); err != nil {
		t.Fatalf("RemoveStaleTemps: %v", err)
	}

	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale temp past the unreadable dir still exists")
	}
}

func TestRemoveStaleTempsMissingRoot(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "nonexistent"))
	if err := s.RemoveStaleTemps(); err == nil {
		t.Error("RemoveStaleTemps on a missing root: want an error")
	}
}
