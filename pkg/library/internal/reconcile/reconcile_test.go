package reconcile

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ramblingenzyme/ebookfs/internal/book"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/index"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/store"
)

func TestStoreDrifted(t *testing.T) {
	tests := []struct {
		name   string
		change func(*testing.T, string, book.Location)
	}{
		{"new directory", func(t *testing.T, root string, _ book.Location) {
			writeBookFiles(t, root, book.Location{EpubPath: filepath.Join("Other", "New (2)", "new.epub")})
		}},
		{"removed directory", func(t *testing.T, root string, loc book.Location) {
			if err := os.RemoveAll(filepath.Join(root, loc.Dir())); err != nil {
				t.Fatal(err)
			}
		}},
		{"epub changed with same size", func(t *testing.T, root string, loc book.Location) {
			path := filepath.Join(root, loc.EpubPath)
			if err := os.WriteFile(path, []byte("xxxx"), 0644); err != nil {
				t.Fatal(err)
			}
			mt := time.Unix(1, 0)
			if err := os.Chtimes(path, mt, mt); err != nil {
				t.Fatal(err)
			}
		}},
		{"epub resized with same mtime", func(t *testing.T, root string, loc book.Location) {
			path := filepath.Join(root, loc.EpubPath)
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("larger epub"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(path, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
		}},
		{"epub renamed", func(t *testing.T, root string, loc book.Location) {
			from := filepath.Join(root, loc.EpubPath)
			to := filepath.Join(filepath.Dir(from), "renamed.epub")
			if err := os.Rename(from, to); err != nil {
				t.Fatal(err)
			}
		}},
		{"sidecar changed", func(t *testing.T, root string, loc book.Location) {
			path := filepath.Join(root, loc.Dir(), "meta.toml")
			if err := os.WriteFile(path, []byte("id = 1\nstatus = \"read\"\n"), 0644); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, files, idx, loc := cleanStoreIndex(t)
			defer idx.Close()
			tc.change(t, root, loc)
			scan, changed := storeDrifted(files, idx)
			if !changed || scan == nil {
				t.Fatalf("storeDrifted = (%v, %v), want drift and a reusable scan", changed, scan)
			}
		})
	}
}

func TestNeedsReindexWhenIndexCheckFails(t *testing.T) {
	idx, err := index.Open(filepath.Join(t.TempDir(), "index.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := idx.Close(); err != nil {
		t.Fatal(err)
	}
	if !needsReindex(idx) {
		t.Fatal("needsReindex = false after index check failure, want rebuild")
	}
}

func TestStoreDriftedCleanScanIsReusable(t *testing.T) {
	_, files, idx, _ := cleanStoreIndex(t)
	defer idx.Close()

	scan, changed := storeDrifted(files, idx)
	if changed || scan == nil || len(scan.entries) != 1 {
		t.Fatalf("storeDrifted = (%v, %v), want clean and its one-entry scan", changed, scan)
	}
}

func cleanStoreIndex(t *testing.T) (string, *store.Store, *index.Index, book.Location) {
	t.Helper()
	root := t.TempDir()
	files := store.New(root)
	idx, err := index.Open(filepath.Join(t.TempDir(), "index.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	loc := book.Location{EpubPath: filepath.Join("Author", "Title (1)", "book.epub")}
	writeBookFiles(t, root, loc)
	info, err := files.Stat(loc)
	if err != nil {
		idx.Close()
		t.Fatal(err)
	}
	if err := idx.Rebuild(nil, map[string]drift.PathInfo{loc.EpubPath: info}, 1); err != nil {
		idx.Close()
		t.Fatal(err)
	}
	return root, files, idx, loc
}

func writeBookFiles(t *testing.T, root string, loc book.Location) {
	t.Helper()
	bookDir := filepath.Join(root, loc.Dir())
	if err := os.MkdirAll(bookDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookDir, "meta.toml"), []byte("id = 1\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bookDir, loc.Filename()), []byte("epub"), 0644); err != nil {
		t.Fatal(err)
	}
}
