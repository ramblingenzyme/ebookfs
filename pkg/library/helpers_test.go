package library

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ramblingenzyme/ebookfs/internal/testing/util"
	"github.com/ramblingenzyme/ebookfs/pkg/library/internal/drift"
)

var buildTestEpub = util.BuildTestEpub

func testConfig(t *testing.T) Config {
	t.Helper()
	return Config(util.TestConfig(t))
}

func openTestLibrary(t *testing.T) *Library {
	t.Helper()
	return openLib(t, testConfig(t))
}

func openLib(t *testing.T, cfg Config, opts ...Option) *Library {
	t.Helper()
	lib, err := Open(cfg, opts...)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { lib.Close() })
	return lib
}

// The store keeps the sidecar's filename private, so tests restate it here
// once.
func metaPathOf(book *Book, root string) string {
	return filepath.Join(root, book.Dir(), "meta.toml")
}

// store.Walk still lists the directory, but os.Stat follows the dangling
// symlink and fails. That is the only way to reach the rebuild's
// could-not-observe path from a test.
func breakEpub(t *testing.T, book *Book, root string) {
	t.Helper()
	absEpub := filepath.Join(root, book.EpubPath())
	if err := os.Remove(absEpub); err != nil {
		t.Fatalf("remove epub: %v", err)
	}
	dangling := filepath.Join(filepath.Dir(absEpub), "nowhere.epub")
	if err := os.Symlink(dangling, absEpub); err != nil {
		t.Fatalf("symlink: %v", err)
	}
}

// The id sequence lives in the index, so only a rebuild without one depends on
// the store's id reservations.
func dropIndex(t *testing.T, cfg Config) {
	t.Helper()
	// The WAL and shared-memory sidecars would otherwise resurrect the sequence.
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(cfg.IndexPath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatalf("remove index%s: %v", suffix, err)
		}
	}
}

// Reopening twice verifies that skipped books can be reconciled repeatedly.
func assertSettlesClean(t *testing.T, cfg Config) {
	t.Helper()
	for i := 1; i <= 2; i++ {
		lib, err := Open(cfg)
		if err != nil {
			t.Fatalf("reopen %d: %v", i, err)
		}
		entries, err := lib.store.Walk()
		if err != nil {
			lib.Close()
			t.Fatalf("walk after reopen %d: %v", i, err)
		}
		indexed, err := lib.index.AllPathInfo()
		if err != nil {
			lib.Close()
			t.Fatalf("indexed paths after reopen %d: %v", i, err)
		}
		if len(indexed) != len(entries) {
			lib.Close()
			t.Fatalf("indexed %d paths after reopen %d, store has %d", len(indexed), i, len(entries))
		}
		for _, entry := range entries {
			observed, err := lib.store.Stat(entry)
			if err != nil {
				observed = drift.PathInfo{}
			}
			if got, ok := indexed[entry.EpubPath]; !ok || !got.Equal(observed) {
				lib.Close()
				t.Fatalf("indexed observation for %q after reopen %d = %+v, want %+v", entry.EpubPath, i, got, observed)
			}
		}
		if err := lib.Close(); err != nil {
			t.Fatalf("Close %d: %v", i, err)
		}
	}
}

func ingestTestEpub(t *testing.T, lib *Library, data []byte) *Book {
	t.Helper()
	b, err := tryIngest(t, lib, data)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	return b
}

// tryIngest returns Ingest's error, and fails the test on any earlier step.
func tryIngest(t *testing.T, lib *Library, data []byte) (*Book, error) {
	//goland:noinspection DuplicatedCode
	t.Helper()
	h, err := lib.CreateIngest()
	if err != nil {
		t.Fatalf("CreateIngest: %v", err)
	}
	if _, err := h.WriteAt(data, 0); err != nil {
		t.Fatalf("WriteAt: %v", err)
	}
	return h.Ingest()
}

// ingestStopped ingests a book into a library at cfg and closes it, so the
// test can change the disk while no library holds it open.
func ingestStopped(t *testing.T, cfg Config, title string, authors ...string) *Book {
	t.Helper()
	lib := openLib(t, cfg)
	b := ingestTestEpub(t, lib, buildTestEpub(t, title, authors...))
	if err := lib.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
