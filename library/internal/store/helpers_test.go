package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

// newStore returns a Store rooted at a fresh temp dir, plus that root.
func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	return New(root, filepath.Join(root, ".inbox-tmp")), root
}

// writeBook materializes an on-disk book directory under root/libPath: the epub
// (skipped when epubName is empty) and a meta.toml (skipped when meta is nil).
func writeBook(t *testing.T, root, libPath, epubName, content string, meta *book.Meta) {
	t.Helper()
	dir := filepath.Join(root, libPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if epubName != "" {
		if err := os.WriteFile(filepath.Join(dir, epubName), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if meta != nil {
		if err := writeMeta(filepath.Join(dir, metaFilename), meta); err != nil {
			t.Fatal(err)
		}
	}
}
