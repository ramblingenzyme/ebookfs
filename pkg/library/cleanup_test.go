package library

import (
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
