package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// TestCleanStaleTmpFiles verifies that stale .ebookfs-*.epub.tmp files are
// removed from book directories during startup. A crash mid-write leaves these
// behind; without cleanup they accumulate forever.
func TestCleanStaleTmpFiles(t *testing.T) {
	root := t.TempDir()

	// Create a book directory structure with stale tmp files
	bookDir := filepath.Join(root, "Author", "Title (1)")
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create stale tmp files
	stale1 := filepath.Join(bookDir, ".ebookfs-12345.epub.tmp")
	stale2 := filepath.Join(bookDir, ".ebookfs-67890.epub.tmp")
	if err := os.WriteFile(stale1, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stale2, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a legitimate epub file (should not be removed)
	epub := filepath.Join(bookDir, "book.epub")
	if err := os.WriteFile(epub, []byte("epub"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create a file with similar name but not matching pattern (should not be removed)
	other := filepath.Join(bookDir, "other.tmp")
	if err := os.WriteFile(other, []byte("other"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run cleanup
	if err := cleanStaleTmpFiles(root); err != nil {
		t.Fatalf("cleanStaleTmpFiles: %v", err)
	}

	// Verify stale files are removed
	if _, err := os.Stat(stale1); !os.IsNotExist(err) {
		t.Errorf("stale1 still exists after cleanup")
	}
	if _, err := os.Stat(stale2); !os.IsNotExist(err) {
		t.Errorf("stale2 still exists after cleanup")
	}

	// Verify legitimate files remain
	if _, err := os.Stat(epub); err != nil {
		t.Errorf("epub was removed: %v", err)
	}
	if _, err := os.Stat(other); err != nil {
		t.Errorf("other.tmp was removed: %v", err)
	}
}

func TestCleanStaleTmpFilesMeta(t *testing.T) {
	root := t.TempDir()
	stale := filepath.Join(root, ".ebookfs-123456789.meta.toml.tmp")
	real := filepath.Join(root, "meta.toml")
	for _, path := range []string{stale, real} {
		if err := os.WriteFile(path, nil, 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := cleanStaleTmpFiles(root); err != nil {
		t.Fatalf("cleanStaleTmpFiles: %v", err)
	}

	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale meta temp still exists after cleanup")
	}
	if _, err := os.Stat(real); err != nil {
		t.Errorf("meta.toml was removed: %v", err)
	}
}

// One unreadable book must not stop the sweep of the books after it.
func TestCleanStaleTmpFilesPastUnreadableDir(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	root := t.TempDir()
	unreadable := filepath.Join(root, "A")
	later := filepath.Join(root, "Z")
	for _, dir := range []string{unreadable, later} {
		if err := os.Mkdir(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	stale := filepath.Join(later, ".ebookfs-1.epub.tmp")
	if err := os.WriteFile(stale, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(unreadable, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(unreadable, 0755) })

	if err := cleanStaleTmpFiles(root); err != nil {
		t.Fatalf("cleanStaleTmpFiles: %v", err)
	}
	if _, err := os.Stat(stale); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("stale temp past the unreadable dir still exists")
	}
}

// TestCleanStaleTmpFilesEmpty verifies cleanup works on an empty directory.
func TestCleanStaleTmpFilesEmpty(t *testing.T) {
	root := t.TempDir()
	if err := cleanStaleTmpFiles(root); err != nil {
		t.Fatalf("cleanStaleTmpFiles on empty dir: %v", err)
	}
}

// TestCleanStaleTmpFilesMissing verifies cleanup returns an error for missing directory.
func TestCleanStaleTmpFilesMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "nonexistent")
	if err := cleanStaleTmpFiles(missing); err == nil {
		t.Error("cleanStaleTmpFiles on missing dir: want an error")
	}
}
