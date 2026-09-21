package store

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/book"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	return New(root), root
}

// writeBook skips the epub on an empty epubName and the sidecar on a nil meta.
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
